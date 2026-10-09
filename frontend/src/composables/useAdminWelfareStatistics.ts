import { computed, onBeforeUnmount, onMounted, reactive, ref, shallowRef, watch } from 'vue'
import { adminWelfareAPI } from '@/api/admin/welfare'
import { useAuthStore } from '@/stores/auth'
import type { WelfareRewardType, WelfareStatisticsQuery, WelfareStatisticsUser, WelfareUserSort } from '@/types/adminWelfare'
import { validWelfareDateRange, welfareDateRange, type WelfareDatePreset } from '@/utils/welfareStatisticsDate'
import { sameWelfareIdentity, welfareIdentity } from '@/utils/welfareSession'

function statisticsResource<T>(fetch: (signal: AbortSignal) => Promise<T>, ensureIdentity: () => boolean) {
  const data = shallowRef<T | null>(null)
  const loading = ref(false)
  const error = ref(false)
  let controller: AbortController | null = null
  let generation = 0
  async function load() {
    if (!ensureIdentity()) return
    controller?.abort()
    const current = ++generation
    controller = new AbortController()
    loading.value = true
    error.value = false
    data.value = null
    try {
      const result = await fetch(controller.signal)
      if (ensureIdentity() && current === generation) data.value = result
    } catch {
      if (ensureIdentity() && current === generation) error.value = true
    } finally {
      if (ensureIdentity() && current === generation) loading.value = false
    }
  }
  function dispose() {
    generation++; controller?.abort(); controller = null
    data.value = null; loading.value = false; error.value = false
  }
  return { data, loading, error, load, dispose }
}

export function useAdminWelfareStatistics() {
  const auth = useAuthStore()
  const identity = welfareIdentity()
  const sessionRevision = auth.sessionRevision
  // The shared session helper can retain a volatile login after a storage write fails.
  // Also observe the persisted marker so a later cross-tab login cannot be hidden.
  let storedSessionMarker: string | null | undefined
  try { storedSessionMarker = localStorage.getItem('auth_session_id') } catch { /* An unreadable owner fails closed. */ }
  const sessionInvalidated = ref(false)
  let disposed = false
  const initialRange = welfareDateRange('last7')
  const draft = reactive({ ...initialRange, preset: 'last7' as WelfareDatePreset, search: '', user_id: '' })
  const applied = ref<WelfareStatisticsQuery>({ ...initialRange })
  const validationError = ref<'invalidRange' | 'invalidUserId' | ''>('')
  const userPage = ref(1)
  const userPageSize = ref(20)
  const sortBy = ref<WelfareUserSort>('total_amount')
  const sortOrder = ref<'asc' | 'desc'>('desc')
  const recordPage = ref(1)
  const recordPageSize = ref(20)
  const recordType = ref<WelfareRewardType>('all')
  const detailDates = reactive({ ...initialRange })
  const detailUser = ref<Pick<WelfareStatisticsUser, 'user_id' | 'email'> | null>(null)
  const recordDateError = ref(false)
  const recordQuery = computed(() => ({
    ...applied.value,
    ...detailDates,
    ...(detailUser.value ? { user_id: detailUser.value.user_id } : {}),
    type: recordType.value, page: recordPage.value, page_size: recordPageSize.value
  }))
  const overview = statisticsResource(signal => adminWelfareAPI.getStatistics({ ...applied.value }, signal), ensureIdentity)
  const users = statisticsResource(signal => adminWelfareAPI.getStatisticsUsers({ ...applied.value, page: userPage.value, page_size: userPageSize.value, sort_by: sortBy.value, sort_order: sortOrder.value }, signal), ensureIdentity)
  const records = statisticsResource(signal => adminWelfareAPI.getStatisticsRecords(recordQuery.value, signal), ensureIdentity)
  const hasDetailScope = computed(() => detailUser.value !== null || detailDates.date_from !== applied.value.date_from || detailDates.date_to !== applied.value.date_to)

  function ensureIdentity() {
    if (disposed || sessionInvalidated.value) return false
    let storedAdministrator = false
    try { storedAdministrator = storedSessionMarker !== undefined && storedSessionMarker === localStorage.getItem('auth_session_id') && !!localStorage.getItem('auth_token') && JSON.parse(localStorage.getItem('auth_user') || 'null')?.role === 'admin' } catch { /* Unreadable authentication fails closed. */ }
    if (identity.userID !== null && sameWelfareIdentity(identity, welfareIdentity()) && sessionRevision === auth.sessionRevision && identity.userID === auth.user?.id && auth.isAuthenticated && auth.isAdmin && storedAdministrator) return true
    // This panel belongs to its original login. A replacement login must reopen it.
    sessionInvalidated.value = true
    overview.dispose(); users.dispose(); records.dispose()
    Object.assign(draft, { ...initialRange, preset: 'last7', search: '', user_id: '' })
    applied.value = { ...initialRange }
    validationError.value = ''
    userPage.value = 1; userPageSize.value = 20; sortBy.value = 'total_amount'; sortOrder.value = 'desc'
    recordPageSize.value = 20
    resetDetails()
    return false
  }
  function selectPreset(preset: WelfareDatePreset) {
    if (!ensureIdentity()) return
    draft.preset = preset
    if (preset !== 'custom') Object.assign(draft, welfareDateRange(preset))
  }
  function resetDetails() {
    Object.assign(detailDates, { date_from: applied.value.date_from, date_to: applied.value.date_to })
    detailUser.value = null
    recordPage.value = 1
    recordType.value = 'all'
    recordDateError.value = false
  }
  function applyFilters() {
    if (!ensureIdentity()) return
    // Presets are recomputed at application so an open page crossing midnight uses the current business day.
    if (draft.preset !== 'custom') Object.assign(draft, welfareDateRange(draft.preset))
    if (!validWelfareDateRange(draft.date_from, draft.date_to)) { validationError.value = 'invalidRange'; return }
    const id = draft.user_id.trim()
    if (id && (!/^[1-9]\d*$/.test(id) || !Number.isSafeInteger(Number(id)))) { validationError.value = 'invalidUserId'; return }
    validationError.value = ''
    applied.value = { date_from: draft.date_from, date_to: draft.date_to, ...(draft.search.trim() ? { search: draft.search.trim() } : {}), ...(id ? { user_id: Number(id) } : {}) }
    userPage.value = 1
    resetDetails()
    void overview.load(); void users.load(); void records.load()
  }
  function drillDate(date: string) { if (!ensureIdentity()) return; Object.assign(detailDates, { date_from: date, date_to: date }); recordPage.value = 1; recordDateError.value = false; void records.load() }
  function drillUser(user: WelfareStatisticsUser) { if (!ensureIdentity()) return; detailUser.value = { user_id: user.user_id, email: user.email }; recordPage.value = 1; void records.load() }
  function clearDetailScope() { if (!ensureIdentity()) return; resetDetails(); void records.load() }
  function setRecordDates(from: string, to: string) {
    if (!ensureIdentity()) return
    recordDateError.value = !validWelfareDateRange(from, to) || from < applied.value.date_from || to > applied.value.date_to
    if (recordDateError.value) return
    Object.assign(detailDates, { date_from: from, date_to: to }); recordPage.value = 1; void records.load()
  }
  function setRecordType(type: WelfareRewardType) { if (!ensureIdentity()) return; recordType.value = type; recordPage.value = 1; void records.load() }
  function setUserPage(page: number) { if (!ensureIdentity()) return; userPage.value = page; void users.load() }
  function setUserPageSize(size: number) { if (!ensureIdentity()) return; userPageSize.value = Math.min(100, size); userPage.value = 1; void users.load() }
  function setRecordPage(page: number) { if (!ensureIdentity()) return; recordPage.value = page; void records.load() }
  function setRecordPageSize(size: number) { if (!ensureIdentity()) return; recordPageSize.value = Math.min(100, size); recordPage.value = 1; void records.load() }
  function sortUsers(key: string, order: 'asc' | 'desc') {
    if (!ensureIdentity()) return
    if (!['total_amount', 'period_total_amount', 'checkin_count'].includes(key)) return
    sortBy.value = key as WelfareUserSort; sortOrder.value = order; userPage.value = 1; void users.load()
  }
  function storageChanged(event: StorageEvent) { if (!event.key || ['auth_session_id', 'auth_user', 'auth_token'].includes(event.key)) ensureIdentity() }
  function foreground() { ensureIdentity() }
  const stopAuthWatch = watch([() => auth.sessionRevision, () => auth.user?.id, () => auth.user?.role, () => auth.isAuthenticated], ensureIdentity, { flush: 'sync' })
  onMounted(() => {
    window.addEventListener('storage', storageChanged)
    window.addEventListener('focus', foreground)
    document.addEventListener('visibilitychange', foreground)
    void overview.load(); void users.load(); void records.load()
  })
  onBeforeUnmount(() => {
    disposed = true; stopAuthWatch()
    overview.dispose(); users.dispose(); records.dispose()
    window.removeEventListener('storage', storageChanged)
    window.removeEventListener('focus', foreground)
    document.removeEventListener('visibilitychange', foreground)
  })
  return { draft, applied, validationError, sessionInvalidated, overview, users, records, userPage, userPageSize, sortBy, sortOrder, recordPage, recordPageSize, recordType, detailDates, detailUser, recordDateError, hasDetailScope, selectPreset, applyFilters, drillDate, drillUser, clearDetailScope, setRecordDates, setRecordType, setUserPage, setUserPageSize, setRecordPage, setRecordPageSize, sortUsers }
}
