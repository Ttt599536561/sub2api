import { computed, onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { useQQSession } from './useQQSession'
import { inviteError } from '@/utils/qqInvite'

export function useInviteAdmin(clear: () => void) {
  const busy = ref(false), error = ref(''), notice = ref('')
  const session = useQQSession(() => { clear(); busy.value = false; error.value = ''; notice.value = '' }, true)
  const blocked = computed(() => busy.value || session.invalidated.value)
  async function run(action: () => Promise<void>) {
    if (busy.value || !session.current()) return
    busy.value = true; error.value = ''; notice.value = ''
    try { await action() } catch (cause) { if (session.current()) error.value = inviteError(cause) }
    finally { if (session.current()) busy.value = false }
  }
  return { session, busy, blocked, error, notice, run }
}

export function useInviteBusy(busy: Readonly<Ref<boolean>>, changed: (value: boolean) => void) {
  watch(busy, changed, { immediate: true, flush: 'sync' })
  onBeforeUnmount(() => changed(false))
}
