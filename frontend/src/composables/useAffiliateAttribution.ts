import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getAffiliateAttribution } from '@/api/auth'
import { setAffiliateAttributionLocked } from '@/utils/oauthAffiliate'
import { extractApiErrorCode } from '@/utils/apiError'

export function useAffiliateAttribution() {
  const { t } = useI18n()
  const locked = ref(true)
  const error = ref('')
  const loading = ref(false)
  let disposed = false
  let request = 0
  onBeforeUnmount(() => { disposed = true; request++ })

  async function refresh() {
    const sequence = ++request
    loading.value = true
    let next = true
    let message = ''
    try {
      next = (await getAffiliateAttribution()).locked === true
    } catch (failure) {
      const code = extractApiErrorCode(failure)
      const known = ['AFFILIATE_ATTRIBUTION_INVALID', 'AFFILIATE_ATTRIBUTION_UNAVAILABLE', 'AFFILIATE_RATE_LIMITED', 'AFFILIATE_DISABLED']
      message = t(`auth.errors.${code && known.includes(code) ? code : 'AFFILIATE_ATTRIBUTION_UNAVAILABLE'}`)
    }
    if (disposed || sequence !== request) return
    locked.value = next
    error.value = message
    loading.value = false
    if (!message) setAffiliateAttributionLocked(next)
  }
  return { locked, error, loading, refresh }
}
