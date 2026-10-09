import { computed, onBeforeUnmount, onMounted, reactive, ref, shallowRef } from 'vue'
import { adminWelfareAPI } from '@/api/admin/welfare'
import type { WelfareRewardType, WelfareStatisticsQuery, WelfareStatisticsUser, WelfareUserSort } from '@/types/adminWelfare'
import { validWelfareDateRange, welfareDateRange, type WelfareDatePreset } from '@/utils/welfareStatisticsDate'

function statisticsResource<T>(fetch: (signal: AbortSignal) => Promise<T>) {
  const data = shallowRef<T | null>(null)
  const loading = ref(false)
  const error = ref(false)
  let controller: AbortController | null = null
  let generation = 0
  async function load() {
    controller?.abort()
    const current = ++generation
    controller = new AbortController()
    loading.value = true
    error.value = false
    data.value = null
    try {
      const result = await fetch(controller.signal)
      if (current === generation) data.value = result
    } catch {
      if (current === generation) error.value = true
    } finally {
      if (current === generation) loading.value = false
    }
  }
  function dispose() { generation++; controller?.abort() }
  return { data, loading, error, load, dispose }
}

export function useAdminWelfareStatistics() {
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
  const overview = statisticsResource(signal => adminWelfareAPI.getStatistics({ ...applied.value }, signal))
  const users = statisticsResource(signal => adminWelfareAPI.getStatisticsUsers({ ...applied.value, page: userPage.value, page_size: userPageSize.value, sort_by: sortBy.value, sort_order: sortOrder.value }, signal))
  const records = statisticsResource(signal => adminWelfareAPI.getStatisticsRecords(recordQuery.value, signal))
  const hasDetailScope = computed(() => detailUser.value !== null || detailDates.date_from !== applied.value.date_from || detailDates.date_to !== applied.value.date_to)

  function selectPreset(preset: WelfareDatePreset) {
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
  function drillDate(date: string) { Object.assign(detailDates, { date_from: date, date_to: date }); recordPage.value = 1; recordDateError.value = false; void records.load() }
  function drillUser(user: WelfareStatisticsUser) { detailUser.value = { user_id: user.user_id, email: user.email }; recordPage.value = 1; void records.load() }
  function clearDetailScope() { resetDetails(); void records.load() }
  function setRecordDates(from: string, to: string) {
    recordDateError.value = !validWelfareDateRange(from, to) || from < applied.value.date_from || to > applied.value.date_to
    if (recordDateError.value) return
    Object.assign(detailDates, { date_from: from, date_to: to }); recordPage.value = 1; void records.load()
  }
  function setRecordType(type: WelfareRewardType) { recordType.value = type; recordPage.value = 1; void records.load() }
  function setUserPage(page: number) { userPage.value = page; void users.load() }
  function setUserPageSize(size: number) { userPageSize.value = Math.min(100, size); userPage.value = 1; void users.load() }
  function setRecordPage(page: number) { recordPage.value = page; void records.load() }
  function setRecordPageSize(size: number) { recordPageSize.value = Math.min(100, size); recordPage.value = 1; void records.load() }
  function sortUsers(key: string, order: 'asc' | 'desc') {
    if (!['total_amount', 'period_total_amount', 'checkin_count'].includes(key)) return
    sortBy.value = key as WelfareUserSort; sortOrder.value = order; userPage.value = 1; void users.load()
  }
  onMounted(() => { void overview.load(); void users.load(); void records.load() })
  onBeforeUnmount(() => { overview.dispose(); users.dispose(); records.dispose() })
  return { draft, applied, validationError, overview, users, records, userPage, userPageSize, sortBy, sortOrder, recordPage, recordPageSize, recordType, detailDates, detailUser, recordDateError, hasDetailScope, selectPreset, applyFilters, drillDate, drillUser, clearDetailScope, setRecordDates, setRecordType, setUserPage, setUserPageSize, setRecordPage, setRecordPageSize, sortUsers }
}
