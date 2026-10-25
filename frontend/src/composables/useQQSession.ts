import { onBeforeUnmount, ref, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { getAuthSessionID } from '@/api/authSession'

/** One page lifetime belongs to one login. Invalidate synchronously, including same-user relogins. */
export function useQQSession(clear: (identityChanged?: boolean) => void, admin = false) {
  const auth = useAuthStore()
  const invalidated = ref(false)
  const controller = new AbortController()
  const owner = { authenticated: auth.isAuthenticated, id: auth.user?.id, role: auth.user?.role, revision: auth.sessionRevision, session: getAuthSessionID(), stored: localStorage.getItem('auth_session_id'), email: auth.user?.email }
  let disposed = false
  function invalidate() {
    if (invalidated.value || disposed) return
    invalidated.value = true; controller.abort(); clear(true)
  }
  function current() {
    if (disposed || invalidated.value) return false
    let storageMatches = false
    try { storageMatches = owner.stored === localStorage.getItem('auth_session_id') && owner.id === JSON.parse(localStorage.getItem('auth_user') || 'null')?.id } catch { /* Fail closed. */ }
    if (!storageMatches || owner.authenticated !== auth.isAuthenticated || owner.session !== getAuthSessionID() || owner.revision !== auth.sessionRevision || owner.id !== auth.user?.id || owner.email !== auth.user?.email || owner.role !== auth.user?.role || (admin && auth.user?.role !== 'admin')) {
      invalidate(); return false
    }
    return auth.isAuthenticated
  }
  watch([() => auth.sessionRevision, () => auth.user?.id, () => auth.user?.role, () => auth.user?.email, () => auth.isAuthenticated], () => { current() }, { flush: 'sync' })
  const inspect = () => { current() }
  window.addEventListener('storage', inspect)
  window.addEventListener('focus', inspect)
  onBeforeUnmount(() => {
    disposed = true; controller.abort(); clear(false)
    window.removeEventListener('storage', inspect); window.removeEventListener('focus', inspect)
  })
  return { invalidated, current, signal: controller.signal }
}
