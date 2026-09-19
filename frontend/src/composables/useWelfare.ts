import { onMounted, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { welfareAPI } from '@/api/welfare'
import { welfareErrorCode } from '@/utils/welfareError'
import { welfareIdentity, sameWelfareIdentity, loadWelfarePending, saveWelfarePending, type WelfarePendingRedemption } from '@/utils/welfareSession'
import type { WelfareOverview, WelfareCalendar, WelfareRecords, WelfareRules, WelfareOperation, WelfareQuoteRequest, WelfareRecordQuery, WelfareRedemption } from '@/types/welfare'

function operationKey() {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
}
function isUncertain(error: unknown) {
  const e = error as { status?: number; response?: { status?: number } }
  const status = e?.status ?? e?.response?.status
  return !status || status >= 500 || status === 408
}
export function useWelfare(initialFilters: Partial<WelfareRecordQuery> = {}) {
  const auth = useAuthStore()
  const overview = ref<WelfareOverview | null>(null)
  const calendar = ref<WelfareCalendar | null>(null)
  const records = ref<WelfareRecords | null>(null)
  const rules = ref<WelfareRules | null>(null)
  const month = ref('')
  const filters = reactive<WelfareRecordQuery>({ type: 'all', date_from: '', date_to: '', page: 1, page_size: 10, ...initialFilters })
  const loading = ref(true)
  const calendarLoading = ref(false)
  const recordsLoading = ref(false)
  const error = ref('')
  const rulesError = ref('')
  const calendarError = ref('')
  const recordsError = ref('')
  const mutationError = ref('')
  const busy = ref(false)
  const sessionInvalidated = ref(false)
  const recovering = ref(false)
  const recovered = ref<Array<{ kind: 'draw' | 'redeem'; result: WelfareOperation; amount?: string }>>([])
  let identity = welfareIdentity()
  const restored = loadWelfarePending(identity)
  const pendingDraw = ref<string | null>(restored.drawKey)
  const pendingRedemption = ref<WelfarePendingRedemption | null>(restored.redemption)
  let epoch = 0
  let disposed = false
  let controller = new AbortController()
  let reading: Promise<void> | null = null
  let calendarSequence = 0
  let recordsSequence = 0
  let timer: ReturnType<typeof setInterval> | undefined
  let failures = 0
  let nextPollAt = 0
  function ensureIdentity() {
    if (disposed || sessionInvalidated.value) return false
    if (!sameWelfareIdentity(identity, welfareIdentity()) || identity.userID !== auth.user?.id) {
      epoch++; controller.abort(); reading = null
      overview.value = null; calendar.value = null; records.value = null; rules.value = null
      pendingDraw.value = null; pendingRedemption.value = null; recovered.value = []
      busy.value = false; loading.value = false; recovering.value = false
      sessionInvalidated.value = true
      return false
    }
    return auth.isAuthenticated
  }
  const current = (owner: number) => !disposed && owner === epoch && ensureIdentity()
  function persistPending() {
    const saved = saveWelfarePending(identity, { drawKey: pendingDraw.value, redemption: pendingRedemption.value })
    if (!saved) mutationError.value = 'WELFARE_STORAGE_UNAVAILABLE'
    return saved
  }
  function accept(next: WelfareOverview, refreshRelated = false) {
    const old = overview.value
    if (old && (next.welfare_balance_version < old.welfare_balance_version || next.wallet_version < old.wallet_version)) return
    // Replayed operations contain historical snapshots, including the old day
    // and program state. Only a fresh read may replace an equal-version state.
    if (old && !refreshRelated && next.welfare_balance_version === old.welfare_balance_version && next.wallet_version === old.wallet_version) return
    overview.value = next
    if (!month.value) month.value = next.business_date.slice(0, 7)
    if (refreshRelated && old) {
      const calendarChanged = old.business_date !== next.business_date || old.total_checkin_days !== next.total_checkin_days || old.today_checked_in !== next.today_checked_in
      if (calendarChanged) {
        if (old.business_date.slice(0, 7) !== next.business_date.slice(0, 7) && month.value === old.business_date.slice(0, 7)) month.value = next.business_date.slice(0, 7)
        else void loadCalendar()
      }
      if (old.welfare_balance_version !== next.welfare_balance_version || old.draws_used !== next.draws_used) void loadRecords()
      if (old.rules_version !== next.rules_version) void loadRules()
    }
  }
  async function loadCalendar() {
    if (!month.value || !ensureIdentity()) return
    const owner = epoch; const sequence = ++calendarSequence
    calendarLoading.value = true; calendarError.value = ''
    try {
      const next = await welfareAPI.calendar(month.value, controller.signal)
      if (current(owner) && sequence === calendarSequence) calendar.value = next
    } catch (e) { if (current(owner) && sequence === calendarSequence) calendarError.value = welfareErrorCode(e) }
    finally { if (current(owner) && sequence === calendarSequence) calendarLoading.value = false }
  }
  async function loadRecords() {
    if (!ensureIdentity()) return
    const owner = epoch; const sequence = ++recordsSequence
    if (filters.date_from && filters.date_to && filters.date_from > filters.date_to) { recordsError.value = 'dateRange'; recordsLoading.value = false; return }
    recordsLoading.value = true; recordsError.value = ''
    try {
      const next = await welfareAPI.records({ ...filters }, controller.signal)
      if (current(owner) && sequence === recordsSequence) records.value = next
    } catch (e) { if (current(owner) && sequence === recordsSequence) recordsError.value = welfareErrorCode(e) }
    finally { if (current(owner) && sequence === recordsSequence) recordsLoading.value = false }
  }
  async function loadRules() {
    if (!ensureIdentity()) return
    const owner = epoch
    try { const next = await welfareAPI.rules(controller.signal); if (current(owner)) { rules.value = next; rulesError.value = '' } }
    catch (e) { if (current(owner)) rulesError.value = welfareErrorCode(e) }
  }
  function refresh(): Promise<void> {
    if (!ensureIdentity()) return Promise.resolve()
    if (reading) return reading
    const owner = epoch
    const task = (async () => {
      try { const next = await welfareAPI.overview(controller.signal); if (current(owner)) { accept(next, true); error.value = ''; failures = 0; nextPollAt = 0 } }
      catch (e) {
        if (current(owner)) {
          error.value = welfareErrorCode(e)
          failures++
          const failure = e as { retryAfter?: string; response?: { headers?: Record<string, string> } }
          const retryAfter = failure.retryAfter ?? failure.response?.headers?.['retry-after']
          const headerDelay = retryAfter ? (/^\d+$/.test(retryAfter) ? Number(retryAfter) * 1000 : Math.max(0, Date.parse(retryAfter) - Date.now())) : 0
          const delay = Math.min(300000, Math.max(Math.min(60000, 10000 * 2 ** Math.min(failures, 3)), Number.isFinite(headerDelay) ? headerDelay : 0))
          nextPollAt = Date.now() + delay
        }
      }
      finally { if (current(owner)) loading.value = false }
    })()
    reading = task
    void task.finally(() => { if (reading === task) reading = null })
    return task
  }
  async function refreshDetails() { await Promise.allSettled([loadCalendar(), loadRecords()]) }
  async function mutate(run: (signal: AbortSignal) => Promise<WelfareOperation>, done?: () => void) {
    if (busy.value || !ensureIdentity()) return null
    const owner = epoch
    busy.value = true; mutationError.value = ''
    try {
      const result = await run(controller.signal)
      if (!current(owner)) return null
      accept(result.overview)
      if (result.status === 'completed') done?.()
      // The POST has committed. Read failures must never turn it into a failed operation.
      void refresh().then(refreshDetails)
      return result
    } catch (e) {
      if (current(owner)) { mutationError.value = welfareErrorCode(e); if (!isUncertain(e)) done?.() }
      return null
    } finally { if (current(owner)) busy.value = false }
  }
  const checkIn = () => mutate(signal => welfareAPI.checkIn(signal))
  function draw() {
    if (!ensureIdentity() || busy.value || recovering.value) return Promise.resolve(null)
    pendingDraw.value ??= operationKey()
    if (!persistPending()) return Promise.resolve(null)
    return mutate(signal => welfareAPI.draw(pendingDraw.value!, signal), () => { pendingDraw.value = null; persistPending() })
  }
  function redeem(body: WelfareRedemption) {
    if (!ensureIdentity() || busy.value || recovering.value) return Promise.resolve(null)
    // An uncertain transfer must keep both its key and its original request body.
    if (pendingRedemption.value && (body.amount !== pendingRedemption.value.body.amount || body.welfare_balance_version !== pendingRedemption.value.body.welfare_balance_version)) {
      mutationError.value = 'WELFARE_OPERATION_UNRESOLVED'
      return Promise.resolve(null)
    }
    pendingRedemption.value ??= { key: operationKey(), body: { ...body } }
    if (!persistPending()) return Promise.resolve(null)
    return mutate(signal => welfareAPI.redeem(pendingRedemption.value!.body, pendingRedemption.value!.key, signal), () => { pendingRedemption.value = null; persistPending() })
  }
  async function quote(body: WelfareQuoteRequest) {
    if (!ensureIdentity()) return null
    const owner = epoch
    const result = await welfareAPI.quote(body, controller.signal)
    return current(owner) ? result : null
  }
  async function recoverPending() {
    if (!ensureIdentity() || recovering.value || busy.value) return
    const owner = epoch
    const pending = [pendingDraw.value ? { kind: 'draw' as const, key: pendingDraw.value } : null, pendingRedemption.value ? { kind: 'redeem' as const, key: pendingRedemption.value.key, amount: pendingRedemption.value.body.amount } : null].filter(item => item !== null)
    if (!pending.length) return
    recovering.value = true
    await Promise.allSettled(pending.map(async item => {
      try {
        const result = await welfareAPI.operationByKey(item.kind, item.key, controller.signal)
        if (!current(owner) || result.status !== 'completed') return
        accept(result.overview)
        if (item.kind === 'draw' && pendingDraw.value === item.key) pendingDraw.value = null
        if (item.kind === 'redeem' && pendingRedemption.value?.key === item.key) pendingRedemption.value = null
        persistPending()
        recovered.value.push({ kind: item.kind, result, amount: item.amount })
      } catch (e) {
        if (current(owner) && (e as { status?: number }).status !== 404) mutationError.value = welfareErrorCode(e)
      }
    }))
    if (current(owner)) { recovering.value = false; void refresh().then(refreshDetails) }
  }
  function activate() {
    void refresh().then(() => { if (!disposed) void loadRecords() })
    void loadRules()
    void recoverPending()
  }
  function foreground() { if (!ensureIdentity()) return; if (document.visibilityState === 'visible' && Date.now() >= nextPollAt) { void refresh(); if (!rules.value) void loadRules(); void recoverPending() } }
  function storageChanged(event: StorageEvent) { if (!event.key || ['auth_session_id', 'auth_user', 'auth_token'].includes(event.key)) ensureIdentity() }
  watch(month, () => { void loadCalendar() })
  watch(filters, () => { void loadRecords() })
  watch(() => [auth.sessionRevision, auth.user?.id], () => {
    if (sessionInvalidated.value) return
    epoch++; controller.abort(); controller = new AbortController(); reading = null
    overview.value = null; calendar.value = null; records.value = null; rules.value = null
    error.value = ''; mutationError.value = ''; busy.value = false; recovering.value = false; loading.value = true
    rulesError.value = ''; calendarError.value = ''; recordsError.value = ''; calendarLoading.value = false; recordsLoading.value = false
    identity = welfareIdentity()
    const restored = loadWelfarePending(identity)
    pendingDraw.value = restored.drawKey; pendingRedemption.value = restored.redemption; recovered.value = []; month.value = ''
    failures = 0; nextPollAt = 0
    if (auth.isAuthenticated) activate()
  })
  onMounted(() => {
    activate()
    timer = setInterval(foreground, 10000)
    window.addEventListener('focus', foreground)
    window.addEventListener('storage', storageChanged)
    document.addEventListener('visibilitychange', foreground)
  })
  onBeforeUnmount(() => {
    disposed = true; epoch++; controller.abort(); clearInterval(timer)
    window.removeEventListener('focus', foreground)
    window.removeEventListener('storage', storageChanged)
    document.removeEventListener('visibilitychange', foreground)
  })
  return { overview, calendar, records, rules, month, filters, loading, calendarLoading, recordsLoading, error, rulesError, calendarError, recordsError, mutationError, busy, sessionInvalidated, recovering, recovered, pendingDraw, pendingRedemption, recoverPending, refresh, loadCalendar, loadRecords, loadRules, checkIn, draw, quote, redeem }
}
