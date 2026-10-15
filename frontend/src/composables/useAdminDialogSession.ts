import { onBeforeUnmount, onMounted, watch } from 'vue'
import { ownedAuthRequestConfig } from '@/api/client'
import { useAuthStore } from '@/stores/auth'

// Scope every asynchronous operation to its dialog target and administrator login.
export function useAdminDialogSession(
  visible: () => boolean,
  target: () => number | undefined,
  reset: () => void,
  closeOnSessionChange: () => void
) {
  const auth = useAuthStore()
  let generation = 0
  let disposed = false
  let sessionInvalidated = false
  let lastOwner = ownedAuthRequestConfig().authIdentity
  function invalidate() {
    generation++
    lastOwner = ownedAuthRequestConfig().authIdentity
    reset()
  }
  function invalidateSession() {
    const shouldClose = visible() && !sessionInvalidated
    // Keep the cleared form unusable until its parent processes close/reopen.
    // An empty local list is not evidence of an empty server list.
    sessionInvalidated = true
    invalidate()
    if (shouldClose) closeOnSessionChange()
  }
  function capture() { return { generation, target: target(), owner: ownedAuthRequestConfig().authIdentity } }
  function isCurrent(request: ReturnType<typeof capture>) {
    const owner = ownedAuthRequestConfig().authIdentity
    return !disposed && !sessionInvalidated && visible() && auth.isAdmin && request.generation === generation && request.target === target() &&
      request.owner.sessionID === owner.sessionID && request.owner.userID === owner.userID &&
      owner.sessionID === lastOwner.sessionID && owner.userID === lastOwner.userID
  }
  function onStorage(event: StorageEvent) {
    if (event.key && !['auth_session_id', 'auth_user'].includes(event.key)) return
    const owner = ownedAuthRequestConfig().authIdentity
    if (owner.sessionID !== lastOwner.sessionID || owner.userID !== lastOwner.userID) invalidateSession()
  }
  watch([visible, target], () => {
    sessionInvalidated = false
    invalidate()
  }, { flush: 'sync' })
  watch([() => auth.user?.id, () => auth.sessionRevision, () => auth.isAdmin], invalidateSession, { flush: 'sync' })
  onMounted(() => window.addEventListener('storage', onStorage))
  onBeforeUnmount(() => { disposed = true; invalidate(); window.removeEventListener('storage', onStorage) })
  return { capture, isCurrent }
}
