import { apiClient } from '@/api/client'
import { CanceledError, type AxiosRequestTransformer } from 'axios'
import { welfareIdentity } from '@/utils/welfareSession'
import type { WelfareStatistics, WelfareStatisticsPage, WelfareStatisticsQuery, WelfareStatisticsRecord, WelfareStatisticsRecordsQuery, WelfareStatisticsUser, WelfareStatisticsUsersQuery } from '@/types/adminWelfare'

export interface WelfareAdminSettings {
  enabled: boolean
  launch_at: string | null
  rules_version: number
}

function owned(signal?: AbortSignal) {
  let sessionMarker: string | null
  try { sessionMarker = localStorage.getItem('auth_session_id') }
  catch { throw new CanceledError('Authentication session is unavailable before statistics dispatch') }
  // Run after queued request interceptors, immediately before dispatch. A volatile
  // session fallback must not mask a persisted login change made by another tab.
  const checkSessionMarker: AxiosRequestTransformer = data => {
    let current = false
    try { current = sessionMarker === localStorage.getItem('auth_session_id') } catch { /* Fail closed. */ }
    if (!current) throw new CanceledError('Authentication session changed before statistics dispatch')
    return data
  }
  const defaults = apiClient.defaults.transformRequest
  return { signal, welfareIdentity: welfareIdentity(), transformRequest: [checkSessionMarker, ...(Array.isArray(defaults) ? defaults : defaults ? [defaults] : [])] }
}
export const adminWelfareAPI = {
  async getStatistics(params: WelfareStatisticsQuery, signal?: AbortSignal): Promise<WelfareStatistics> {
    const { data } = await apiClient.get<WelfareStatistics>('/admin/welfare/statistics', { params, ...owned(signal) })
    return data
  },
  async getStatisticsUsers(params: WelfareStatisticsUsersQuery, signal?: AbortSignal): Promise<WelfareStatisticsPage<WelfareStatisticsUser>> {
    const { data } = await apiClient.get<WelfareStatisticsPage<WelfareStatisticsUser>>('/admin/welfare/statistics/users', { params, ...owned(signal) })
    return data
  },
  async getStatisticsRecords(params: WelfareStatisticsRecordsQuery, signal?: AbortSignal): Promise<WelfareStatisticsPage<WelfareStatisticsRecord>> {
    const { data } = await apiClient.get<WelfareStatisticsPage<WelfareStatisticsRecord>>('/admin/welfare/statistics/records', { params, ...owned(signal) })
    return data
  },
  async getSettings(): Promise<WelfareAdminSettings> {
    const { data } = await apiClient.get<WelfareAdminSettings>('/admin/welfare/settings')
    return data
  },
  async updateSettings(payload: { enabled: boolean }): Promise<WelfareAdminSettings> {
    const { data } = await apiClient.put<WelfareAdminSettings>('/admin/welfare/settings', payload)
    return data
  }
}
