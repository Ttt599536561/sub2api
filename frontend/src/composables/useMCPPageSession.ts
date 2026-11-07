import { onBeforeUnmount, ref, watch } from 'vue'
import { getAuthSessionID } from '@/api/authSession'
import { useAuthStore } from '@/stores/auth'

/** A consent or connections page belongs to one login, including same-user relogins. */
export function useMCPPageSession(clear: () => void) {
  const auth = useAuthStore()
  const invalidated = ref(false)
  const owner = { id: auth.user?.id, role: auth.user?.role, revision: auth.sessionRevision, session: getAuthSessionID() }
  let disposed = false
  function invalidate() {
    if (invalidated.value || disposed) return
    invalidated.value = true
    clear()
  }
  function current(): boolean {
    if (disposed || invalidated.value) return false
    let storedID: unknown
    try { storedID = JSON.parse(localStorage.getItem('auth_user') || 'null')?.id } catch { /* Fail closed. */ }
    if (!auth.isAuthenticated || owner.id == null || storedID !== owner.id ||
      owner.id !== auth.user?.id || owner.role !== auth.user?.role ||
      owner.revision !== auth.sessionRevision || owner.session !== getAuthSessionID()) {
      invalidate()
      return false
    }
    return true
  }
  watch([() => auth.user?.id, () => auth.user?.role, () => auth.sessionRevision, () => auth.isAuthenticated], current, { flush: 'sync' })
  const inspect = () => { current() }
  window.addEventListener('storage', inspect)
  window.addEventListener('focus', inspect)
  onBeforeUnmount(() => {
    disposed = true
    clear()
    window.removeEventListener('storage', inspect)
    window.removeEventListener('focus', inspect)
  })
  return { invalidated, current }
}
