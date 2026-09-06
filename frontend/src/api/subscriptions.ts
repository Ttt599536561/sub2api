/**
 * User Subscription API
 * API for regular users to view their own subscriptions and progress
 */

import { apiClient } from './client'
import type { UserSubscription, SubscriptionProgress, DailyResetMutationResult } from '@/types'

export interface DailyResetRequest { expected_version: number; expected_date: string }
export interface AutoDailyResetRequest extends DailyResetRequest { enabled: boolean }

/**
 * Subscription summary for user dashboard
 */
export interface SubscriptionSummary {
  active_count: number
  subscriptions: Array<{
    id: number
    group_name: string
    status: string
    daily_progress: number | null
    weekly_progress: number | null
    monthly_progress: number | null
    expires_at: string | null
    days_remaining: number | null
  }>
}

/**
 * Get list of current user's subscriptions
 */
export async function getMySubscriptions(): Promise<UserSubscription[]> {
  const response = await apiClient.get<UserSubscription[]>('/subscriptions')
  return response.data
}

/**
 * Get current user's active subscriptions
 */
export async function getActiveSubscriptions(): Promise<UserSubscription[]> {
  const response = await apiClient.get<UserSubscription[]>('/subscriptions/active')
  return response.data
}

/**
 * Get progress for all user's active subscriptions
 */
export async function getSubscriptionsProgress(): Promise<SubscriptionProgress[]> {
  const response = await apiClient.get<SubscriptionProgress[]>('/subscriptions/progress')
  return response.data
}

/**
 * Get subscription summary for dashboard display
 */
export async function getSubscriptionSummary(): Promise<SubscriptionSummary> {
  const response = await apiClient.get<SubscriptionSummary>('/subscriptions/summary')
  return response.data
}

/**
 * Get progress for a specific subscription
 */
export async function getSubscriptionProgress(
  subscriptionId: number
): Promise<SubscriptionProgress> {
  const response = await apiClient.get<SubscriptionProgress>(
    `/subscriptions/${subscriptionId}/progress`
  )
  return response.data
}

export async function resetDailyQuota(
  subscriptionId: number,
  request: DailyResetRequest,
  operationId: string
): Promise<DailyResetMutationResult> {
  const response = await apiClient.post<DailyResetMutationResult>(
    `/subscriptions/${subscriptionId}/reset-daily`, request,
    { headers: { 'Idempotency-Key': operationId } }
  )
  return response.data
}

export async function setAutoDailyReset(
  subscriptionId: number,
  request: AutoDailyResetRequest,
  operationId: string
): Promise<DailyResetMutationResult> {
  const response = await apiClient.put<DailyResetMutationResult>(
    `/subscriptions/${subscriptionId}/auto-daily-reset`, request,
    { headers: { 'Idempotency-Key': operationId } }
  )
  return response.data
}

export async function getDailyResetState(subscriptionId: number): Promise<UserSubscription> {
  const response = await apiClient.get<UserSubscription>(`/subscriptions/${subscriptionId}/daily-reset-state`)
  return response.data
}

export async function getDailyResetOperation(subscriptionId: number, operationId: string): Promise<DailyResetMutationResult> {
  const response = await apiClient.get<DailyResetMutationResult>(
    `/subscriptions/${subscriptionId}/daily-reset-operations/${encodeURIComponent(operationId)}`
  )
  return response.data
}

export default {
  getMySubscriptions,
  getActiveSubscriptions,
  getSubscriptionsProgress,
  getSubscriptionSummary,
  getSubscriptionProgress,
  resetDailyQuota,
  setAutoDailyReset,
  getDailyResetState,
  getDailyResetOperation
}
