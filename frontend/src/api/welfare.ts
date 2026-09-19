import { apiClient } from './client'
import { welfareIdentity } from '@/utils/welfareSession'
import type { WelfareOverview, WelfareCalendar, WelfareRecords, WelfareRecordQuery, WelfareRules, WelfareQuote, WelfareQuoteRequest, WelfareOperation, WelfareRedemption } from '@/types/welfare'

const prefix = '/user/welfare'
const owned = (signal?: AbortSignal) => ({ signal, welfareIdentity: welfareIdentity() })
export const welfareAPI = {
  async overview(signal?: AbortSignal) { return (await apiClient.get<WelfareOverview>(`${prefix}/overview`, owned(signal))).data },
  async calendar(month: string, signal?: AbortSignal) { return (await apiClient.get<WelfareCalendar>(`${prefix}/calendar`, { ...owned(signal), params: { month } })).data },
  async records(params: WelfareRecordQuery, signal?: AbortSignal) { return (await apiClient.get<WelfareRecords>(`${prefix}/records`, { ...owned(signal), params })).data },
  async rules(signal?: AbortSignal) { return (await apiClient.get<WelfareRules>(`${prefix}/rules`, owned(signal))).data },
  async checkIn(signal?: AbortSignal) { return (await apiClient.post<WelfareOperation>(`${prefix}/check-in`, {}, owned(signal))).data },
  async draw(key: string, signal?: AbortSignal) { return (await apiClient.post<WelfareOperation>(`${prefix}/draw`, {}, { ...owned(signal), headers: { 'Idempotency-Key': key } })).data },
  async quote(body: WelfareQuoteRequest, signal?: AbortSignal) { return (await apiClient.post<WelfareQuote>(`${prefix}/redemption-quote`, body, owned(signal))).data },
  async redeem(body: WelfareRedemption, key: string, signal?: AbortSignal) { return (await apiClient.post<WelfareOperation>(`${prefix}/redeem`, body, { ...owned(signal), headers: { 'Idempotency-Key': key } })).data },
  async operation(id: string, signal?: AbortSignal) { return (await apiClient.get<WelfareOperation>(`${prefix}/operations/${encodeURIComponent(id)}`, owned(signal))).data },
  async operationByKey(type: 'draw' | 'redeem', idempotency_key: string, signal?: AbortSignal) { return (await apiClient.get<WelfareOperation>(`${prefix}/operations/by-key`, { ...owned(signal), params: { type, idempotency_key } })).data }
}
