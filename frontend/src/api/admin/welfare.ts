import { apiClient } from '@/api/client'
import type { WelfareStatistics, WelfareStatisticsPage, WelfareStatisticsQuery, WelfareStatisticsRecord, WelfareStatisticsRecordsQuery, WelfareStatisticsUser, WelfareStatisticsUsersQuery } from '@/types/adminWelfare'

export interface WelfareAdminSettings {
  enabled: boolean
  launch_at: string | null
  rules_version: number
}

export const adminWelfareAPI = {
  async getStatistics(params: WelfareStatisticsQuery, signal?: AbortSignal): Promise<WelfareStatistics> {
    const { data } = await apiClient.get<WelfareStatistics>('/admin/welfare/statistics', { params, signal })
    return data
  },
  async getStatisticsUsers(params: WelfareStatisticsUsersQuery, signal?: AbortSignal): Promise<WelfareStatisticsPage<WelfareStatisticsUser>> {
    const { data } = await apiClient.get<WelfareStatisticsPage<WelfareStatisticsUser>>('/admin/welfare/statistics/users', { params, signal })
    return data
  },
  async getStatisticsRecords(params: WelfareStatisticsRecordsQuery, signal?: AbortSignal): Promise<WelfareStatisticsPage<WelfareStatisticsRecord>> {
    const { data } = await apiClient.get<WelfareStatisticsPage<WelfareStatisticsRecord>>('/admin/welfare/statistics/records', { params, signal })
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
