export interface WelfareRewardTotals {
  daily_amount: string
  streak_amount: string
  checkin_amount: string
  draw_amount: string
  total_amount: string
  checkin_users: number
  checkin_count: number
  streak_users: number
  draw_users: number
  draw_count: number
  participating_users: number
}

export interface WelfareStatisticsQuery {
  date_from: string
  date_to: string
  search?: string
  user_id?: number
}
export type WelfareUserSort = 'total_amount' | 'period_total_amount' | 'checkin_count'
export interface WelfareStatisticsUsersQuery extends WelfareStatisticsQuery {
  page: number
  page_size: number
  sort_by: WelfareUserSort
  sort_order: 'asc' | 'desc'
}
export type WelfareRewardType = 'all' | 'daily' | 'streak' | 'draw'
export interface WelfareStatisticsRecordsQuery extends WelfareStatisticsQuery {
  page: number
  page_size: number
  type: WelfareRewardType
}
export interface WelfareStatistics {
  date_from: string
  date_to: string
  timezone: 'Asia/Shanghai'
  summary: WelfareRewardTotals
  daily: Array<WelfareRewardTotals & { date: string }>
}
export interface WelfareStatisticsUser {
  user_id: number
  email: string
  period: WelfareRewardTotals
  lifetime: WelfareRewardTotals
}
export interface WelfareStatisticsRecord {
  id: string
  user_id: number
  email: string
  type: Exclude<WelfareRewardType, 'all'>
  created_at: string
  business_date: string
  amount: string
  cycle_day?: number
}
export interface WelfareStatisticsPage<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}
