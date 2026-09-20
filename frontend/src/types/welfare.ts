export interface WelfareOverview {
  welfare_balance: string
  account_balance: string
  available_draws: number
  draws_used: number
  subscription_draws?: number
  total_checkin_days: number
  cycle_day: number
  today_checked_in: boolean
  business_date: string
  next_reset_at: string
  eligible_spend: string
  next_draw_remaining: string
  ticket_debt: number
  wallet_version: number
  welfare_balance_version: number
  rewards_enabled: boolean
  rules_version: number
  milestones: Array<{ day: number; status: 'locked' | 'available' | 'claimed' }>
}
export interface WelfarePrize { id: string; name: string; amount: string; probability: string }
export interface WelfareRules { prizes: WelfarePrize[]; draw_threshold: string; subscription_draw_threshold?: string; subscription_draw_currency?: string; timezone: string }
export interface WelfareCalendar { month: string; days: Array<{ date: string; checked_in: boolean; reward_amount?: string }> }
export type WelfareRecordType = 'daily' | 'streak' | 'draw' | 'redeem'
export interface WelfareRecord { id: string; type: WelfareRecordType; created_at: string; amount: string; description: string }
export interface WelfareRecords { items: WelfareRecord[]; total: number; page: number; page_size: number; total_draws: number }
export interface WelfareRecordQuery { type: 'all' | WelfareRecordType; date_from: string; date_to: string; page: number; page_size: number }
export interface WelfareQuoteRequest { mode: 'partial' | 'all'; amount?: string }
export interface WelfareQuote { amount: string; welfare_balance: string; account_balance: string; account_balance_after: string; welfare_balance_after?: string; welfare_balance_version: number }
export interface WelfareRedemption { amount: string; welfare_balance_version: number }
export interface WelfareOperation { operation_id: string; status: 'completed' | 'pending'; overview: WelfareOverview; amount?: string; reward_amount?: string; base_reward_amount?: string; streak_reward_amount?: string; prize?: WelfarePrize }
