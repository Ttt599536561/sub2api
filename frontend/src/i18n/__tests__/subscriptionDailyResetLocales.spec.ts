import { describe, expect, it } from 'vitest'
import en from '../locales/en'
import zh from '../locales/zh'

describe('subscription daily reset translations', () => {
  it.each([{ locale: 'en', messages: en, resetLabel: 'Reset' }, { locale: 'zh', messages: zh, resetLabel: '重置' }])(
    'resolves the subscription page controls and feedback in $locale', ({ locale, messages, resetLabel }) => {
      const keys = ['resetDaily', 'autoDailyReset', 'dailyResetCount', 'verifyingReset', 'verifyReset',
        'dailyResetSuccess', 'dailyResetFailed', 'autoDailyResetSaved', 'autoDailyResetSavedCheckFailed']

      for (const key of keys) {
        expect(messages.userSubscriptions, `${locale}: ${key}`).toHaveProperty(key, expect.any(String))
      }
      expect(messages.userSubscriptions.resetDaily).toBe(resetLabel)
      expect(messages.userSubscriptions.dailyResetCount).toContain('{count}/{limit}')
      expect(messages.admin.groups.subscription.allowDayReset).toEqual(expect.any(String))
    }
  )
})
