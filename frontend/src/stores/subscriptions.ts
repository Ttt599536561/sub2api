/**
 * Subscription Store
 * Global state management for user subscriptions with caching and deduplication
 */

import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import subscriptionsAPI from '@/api/subscriptions'
import type { UserSubscription, DailyResetMutationResult } from '@/types'
import type { DailyResetRequest } from '@/api/subscriptions'

// Cache TTL: 60 seconds
const CACHE_TTL_MS = 60_000

// Request generation counter to invalidate stale in-flight responses
interface PendingDailyReset extends DailyResetRequest {
  user_id: number
  subscription_id: number
  operation_id: string
  status: 'pending' | 'checking'
  kind: 'manual' | 'auto'
  enabled?: boolean
}

function pendingKey(userId: number, subscriptionId: number) {
  return `subscription-daily-reset:${userId}:${subscriptionId}`
}

function isDefinitiveFailure(error: unknown): boolean {
  const status = (error as { status?: number })?.status
  return !!status && status >= 400 && status < 500 && status !== 408
}

function newestSubscription(current: UserSubscription | undefined, incoming: UserSubscription) {
  if (!current?.daily_reset || !incoming.daily_reset) return incoming
  const oldState = current.daily_reset
  const newState = incoming.daily_reset
  if (oldState.daily_reset_version > newState.daily_reset_version) return current
  if (oldState.daily_reset_version === newState.daily_reset_version &&
      Date.parse(oldState.server_time) > Date.parse(newState.server_time)) return current
  return incoming
}

export const useSubscriptionStore = defineStore('subscriptions', () => {
  // State
  const activeSubscriptions = ref<UserSubscription[]>([])
  const subscriptions = ref<UserSubscription[]>([])
  const pendingDailyResets = ref<Record<number, PendingDailyReset>>({})
  let requestGeneration = 0
  let listGeneration = 0
  let sessionGeneration = 0
  const loading = ref(false)
  const loaded = ref(false)
  const lastFetchedAt = ref<number | null>(null)

  // In-flight request deduplication
  let activePromise: Promise<UserSubscription[]> | null = null

  // Auto-refresh interval
  let pollerInterval: ReturnType<typeof setInterval> | null = null

  // Computed
  const hasActiveSubscriptions = computed(() => activeSubscriptions.value.length > 0)

  /**
   * Fetch active subscriptions with caching and deduplication
   * @param force - Force refresh even if cache is valid
   */
  async function fetchActiveSubscriptions(force = false): Promise<UserSubscription[]> {
    const now = Date.now()

    // Return cached data if valid
    if (
      !force &&
      loaded.value &&
      lastFetchedAt.value &&
      now - lastFetchedAt.value < CACHE_TTL_MS
    ) {
      return activeSubscriptions.value
    }

    // Return in-flight request if exists (deduplication)
    if (activePromise && !force) {
      return activePromise
    }

    const currentGeneration = ++requestGeneration

    // Start new request
    loading.value = true
    const requestPromise = subscriptionsAPI
      .getActiveSubscriptions()
      .then((data) => {
        if (currentGeneration === requestGeneration) {
          activeSubscriptions.value = data.map(item => newestSubscription(
            activeSubscriptions.value.find(current => current.id === item.id), item
          ))
          loaded.value = true
          lastFetchedAt.value = Date.now()
        }
        return data
      })
      .catch((error) => {
        console.error('Failed to fetch active subscriptions:', error)
        throw error
      })
      .finally(() => {
        if (activePromise === requestPromise) {
          loading.value = false
          activePromise = null
        }
      })

    activePromise = requestPromise

    return activePromise
  }

  async function fetchSubscriptions(): Promise<UserSubscription[]> {
    const generation = ++listGeneration
    const data = await subscriptionsAPI.getMySubscriptions()
    if (generation === listGeneration) {
      subscriptions.value = data.map(item => newestSubscription(
        subscriptions.value.find(current => current.id === item.id), item
      ))
      restorePending(subscriptions.value)
    }
    return subscriptions.value
  }

  function applySubscription(updated: UserSubscription) {
    // Mutations invalidate reads started before the authoritative response arrived.
    requestGeneration++
    listGeneration++
    activePromise = null
    loading.value = false
    lastFetchedAt.value = null
    const index = subscriptions.value.findIndex(item => item.id === updated.id)
    const latest = newestSubscription(subscriptions.value[index], updated)
    if (index < 0) subscriptions.value.push(latest)
    else subscriptions.value[index] = latest

    const activeIndex = activeSubscriptions.value.findIndex(item => item.id === updated.id)
    if (latest.status === 'active') {
      if (activeIndex < 0) activeSubscriptions.value.push(latest)
      else activeSubscriptions.value[activeIndex] = latest
    } else if (activeIndex >= 0) {
      activeSubscriptions.value.splice(activeIndex, 1)
    }
  }

  function clearPending(pending: PendingDailyReset) {
    delete pendingDailyResets.value[pending.subscription_id]
    try {
      sessionStorage.removeItem(pendingKey(pending.user_id, pending.subscription_id))
    } catch {
      // A retained completed operation is safe to query again after remount.
    }
  }

  function restorePending(items: UserSubscription[]) {
    for (const item of items) {
      if (pendingDailyResets.value[item.id]) continue
      try {
        const pending = JSON.parse(sessionStorage.getItem(pendingKey(item.user_id, item.id)) || 'null') as PendingDailyReset | null
        if (pending?.user_id === item.user_id && pending.subscription_id === item.id &&
          typeof pending.operation_id === 'string' && pending.operation_id.length > 0 &&
          Number.isSafeInteger(pending.expected_version) && pending.expected_version >= 0 &&
          typeof pending.expected_date === 'string' &&
          (pending.kind === 'manual' || (pending.kind === 'auto' && typeof pending.enabled === 'boolean'))) {
          pendingDailyResets.value[item.id] = { ...pending, status: 'checking' }
        }
      } catch {
        // Session storage may be unavailable or contain an obsolete record.
      }
    }
  }

  function applyMutation(result: DailyResetMutationResult, pending: PendingDailyReset, generation: number) {
    if (generation !== sessionGeneration) return null
    applySubscription(result.subscription)
    clearPending(pending)
    return result
  }

  function submitPending(pending: PendingDailyReset) {
    const request = { expected_version: pending.expected_version, expected_date: pending.expected_date }
    return pending.kind === 'manual'
      ? subscriptionsAPI.resetDailyQuota(pending.subscription_id, request, pending.operation_id)
      : subscriptionsAPI.setAutoDailyReset(pending.subscription_id,
        { ...request, enabled: pending.enabled! }, pending.operation_id)
  }

  async function rejectOperation(error: unknown, pending: PendingDailyReset, generation: number) {
    if (generation !== sessionGeneration) return null
    clearPending(pending)
    try {
      const current = await subscriptionsAPI.getDailyResetState(pending.subscription_id)
      if (generation === sessionGeneration) applySubscription(current)
    } catch {
      invalidateCache()
    }
    throw error
  }

  async function recoverPending(pending: PendingDailyReset, generation: number): Promise<DailyResetMutationResult | null> {
    try {
      if (pending.kind === 'manual') {
        try {
          const result = await subscriptionsAPI.getDailyResetOperation(pending.subscription_id, pending.operation_id)
          return applyMutation(result, pending, generation)
        } catch (error) {
          if ((error as { status?: number })?.status !== 404) throw error
        }
      } else {
        const current = await subscriptionsAPI.getDailyResetState(pending.subscription_id)
        if (generation !== sessionGeneration) return null
        applySubscription(current)
        if (current.daily_reset?.daily_reset_version !== pending.expected_version ||
          current.daily_reset?.server_date !== pending.expected_date) {
          clearPending(pending)
          return null
        }
      }
    } catch {
      if (generation === sessionGeneration) pendingDailyResets.value[pending.subscription_id].status = 'checking'
      return null
    }

    if (generation !== sessionGeneration) return null
    // A missing event can mean the first request is still committing. Reuse its entire request.
    try {
      return applyMutation(await submitPending(pending), pending, generation)
    } catch (error) {
      if (isDefinitiveFailure(error)) return rejectOperation(error, pending, generation)
      if (generation === sessionGeneration) pendingDailyResets.value[pending.subscription_id].status = 'checking'
      return null
    }
  }

  async function recoverDailyReset(id: number) {
    const pending = pendingDailyResets.value[id]
    if (!pending || pending.status === 'pending') return null
    pending.status = 'pending'
    return recoverPending(pending, sessionGeneration)
  }

  async function startDailyReset(subscription: UserSubscription, enabled?: boolean): Promise<DailyResetMutationResult | null> {
    const state = subscription.daily_reset
    if (!state || pendingDailyResets.value[subscription.id] ||
      (enabled === undefined && (!state.eligible || !state.can_reset))) return null
    const pending: PendingDailyReset = {
      subscription_id: subscription.id, user_id: subscription.user_id,
      operation_id: globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`,
      status: 'pending',
      kind: enabled === undefined ? 'manual' : 'auto', enabled,
      expected_version: state.daily_reset_version, expected_date: state.server_date
    }
    // Persist before submitting: a remount must never create a new ID for an uncertain operation.
    try {
      sessionStorage.setItem(pendingKey(subscription.user_id, subscription.id), JSON.stringify(pending))
    } catch (error) {
      // Disabling cannot charge and must remain available when storage is full.
      if (enabled !== false) throw error
    }
    pendingDailyResets.value[subscription.id] = pending
    const generation = sessionGeneration
    try {
      return applyMutation(await submitPending(pending), pending, generation)
    } catch (error) {
      if (generation !== sessionGeneration) return null
      if (isDefinitiveFailure(error)) return rejectOperation(error, pending, generation)
      return recoverPending(pending, generation)
    }
  }

  function resetDailyQuota(subscription: UserSubscription) {
    return startDailyReset(subscription)
  }

  function setAutoDailyReset(subscription: UserSubscription, enabled: boolean) {
    return startDailyReset(subscription, enabled)
  }

  /**
   * Start auto-refresh polling 
   */
  function startPolling() {
    if (pollerInterval) return

    pollerInterval = setInterval(() => {
      fetchActiveSubscriptions(true).catch((error) => {
        console.error('Subscription polling failed:', error)
      })
    }, 5 * 60 * 1000)
  }

  /**
   * Stop auto-refresh polling
   */
  function stopPolling() {
    if (pollerInterval) {
      clearInterval(pollerInterval)
      pollerInterval = null
    }
  }

  /**
   * Clear all subscription data and stop polling
   */
  function clear() {
    requestGeneration++
    listGeneration++
    sessionGeneration++
    activePromise = null
    activeSubscriptions.value = []
    subscriptions.value = []
    pendingDailyResets.value = {}
    loading.value = false
    loaded.value = false
    lastFetchedAt.value = null
    stopPolling()
  }

  /**
   * Invalidate cache (force next fetch to reload)
   */
  function invalidateCache() {
    lastFetchedAt.value = null
  }

  return {
    // State
    activeSubscriptions,
    subscriptions,
    pendingDailyResets,
    loading,
    hasActiveSubscriptions,

    // Actions
    fetchActiveSubscriptions,
    fetchSubscriptions,
    resetDailyQuota,
    setAutoDailyReset,
    recoverDailyReset,
    startPolling,
    stopPolling,
    clear,
    invalidateCache
  }
})
