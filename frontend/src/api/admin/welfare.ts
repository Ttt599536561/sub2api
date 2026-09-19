import { apiClient } from '@/api/client'

export interface WelfareAdminSettings {
  enabled: boolean
  launch_at: string | null
  rules_version: number
}

export const adminWelfareAPI = {
  async getSettings(): Promise<WelfareAdminSettings> {
    const { data } = await apiClient.get<WelfareAdminSettings>('/admin/welfare/settings')
    return data
  },
  async updateSettings(payload: { enabled: boolean }): Promise<WelfareAdminSettings> {
    const { data } = await apiClient.put<WelfareAdminSettings>('/admin/welfare/settings', payload)
    return data
  }
}
