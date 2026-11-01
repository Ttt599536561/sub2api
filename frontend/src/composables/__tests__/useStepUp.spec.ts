import { afterEach, describe, it, expect, vi } from 'vitest'
import {
  useStepUp,
  isStepUpRequired,
  isStepUpBlocked,
  isStepUpCancelled,
  stepUpBlockReason,
  StepUpCancelledError
} from '../useStepUp'

describe('useStepUp error classification', () => {
  it('detects STEP_UP_REQUIRED from code field', () => {
    expect(isStepUpRequired({ status: 403, code: 'STEP_UP_REQUIRED' })).toBe(true)
    expect(isStepUpRequired({ status: 403, reason: 'STEP_UP_REQUIRED' })).toBe(true)
    expect(isStepUpRequired({ status: 500, code: 'INTERNAL' })).toBe(false)
    expect(isStepUpRequired(null)).toBe(false)
  })

  it('detects blocked (non-retryable) step-up errors', () => {
    expect(isStepUpBlocked({ code: 'STEP_UP_TOTP_NOT_ENABLED' })).toBe(true)
    expect(isStepUpBlocked({ reason: 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' })).toBe(true)
    expect(isStepUpBlocked({ code: 'STEP_UP_REQUIRED' })).toBe(false)
  })

  it('surfaces the block reason marker', () => {
    expect(stepUpBlockReason({ reason: 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' })).toBe('STEP_UP_ADMIN_API_KEY_FORBIDDEN')
    expect(stepUpBlockReason({ code: 'OTHER' })).toBe('')
  })
})

describe('useStepUp concurrent prompts', () => {
  it.each([true, false])('settles every waiter when verification resolves to %s', async (verified) => {
    const stepUp = useStepUp()
    const firstDone = vi.fn()
    const secondDone = vi.fn()
    const first = stepUp.prompt()
    first.then(firstDone)
    stepUp.prompt().then(secondDone)
    if (verified) stepUp.onVerified()
    else stepUp.onCancel()
    await Promise.resolve()
    expect(firstDone).toHaveBeenCalledWith(verified)
    expect(secondDone).toHaveBeenCalledWith(verified)
    expect(stepUp.visible.value).toBe(false)
    const next = stepUp.prompt()
    expect(next).not.toBe(first)
    stepUp.onCancel()
    await expect(next).resolves.toBe(false)
  })
})

describe('useStepUp.run', () => {
  it('returns the action result directly on success', async () => {
    const stepUp = useStepUp()
    const result = await stepUp.run(async () => 42)
    expect(result).toBe(42)
    expect(stepUp.visible.value).toBe(false)
  })

  it('re-throws non-step-up errors without prompting', async () => {
    const stepUp = useStepUp()
    const err = { status: 500, code: 'INTERNAL' }
    await expect(stepUp.run(async () => { throw err })).rejects.toBe(err)
    expect(stepUp.visible.value).toBe(false)
  })

  it('re-throws blocked errors without prompting', async () => {
    const stepUp = useStepUp()
    const err = { status: 403, code: 'STEP_UP_TOTP_NOT_ENABLED' }
    await expect(stepUp.run(async () => { throw err })).rejects.toBe(err)
    expect(stepUp.visible.value).toBe(false)
  })

  it('prompts on STEP_UP_REQUIRED and retries after verification', async () => {
    const stepUp = useStepUp()
    let calls = 0
    const action = async () => {
      calls++
      if (calls === 1) throw { status: 403, code: 'STEP_UP_REQUIRED' }
      return 'ok'
    }
    const promise = stepUp.run(action)
    // The dialog should now be open, awaiting verification (after the first rejection is handled).
    await vi.waitFor(() => expect(stepUp.visible.value).toBe(true))
    stepUp.onVerified()
    await expect(promise).resolves.toBe('ok')
    expect(calls).toBe(2)
    expect(stepUp.visible.value).toBe(false)
  })

  it('throws a cancellation sentinel (not the original error) if the user cancels the prompt', async () => {
    const stepUp = useStepUp()
    const err = { status: 403, code: 'STEP_UP_REQUIRED' }
    const promise = stepUp.run(async () => { throw err })
    await vi.waitFor(() => expect(stepUp.visible.value).toBe(true))
    stepUp.onCancel()
    await expect(promise).rejects.toBeInstanceOf(StepUpCancelledError)
    expect(stepUp.visible.value).toBe(false)
  })

  it('classifies the cancellation sentinel distinctly from step-up errors', () => {
    const cancelled = new StepUpCancelledError()
    expect(isStepUpCancelled(cancelled)).toBe(true)
    expect(isStepUpRequired(cancelled)).toBe(false)
    expect(isStepUpBlocked(cancelled)).toBe(false)
    expect(isStepUpCancelled({ status: 403, code: 'STEP_UP_REQUIRED' })).toBe(false)
  })
})


describe('useStepUp login ownership', () => {
  afterEach(() => localStorage.clear())

  it('does not retry an old sensitive action after the login session changes during verification', async () => {
    localStorage.setItem('auth_session_id', 'original-session')
    const stepUp = useStepUp()
    const action = vi.fn().mockRejectedValueOnce({ code: 'STEP_UP_REQUIRED' }).mockResolvedValue('new-account-result')
    const pending = stepUp.run(action).catch(error => error)
    await Promise.resolve()
    expect(stepUp.visible.value).toBe(true)
    localStorage.setItem('auth_session_id', 'replacement-session')
    stepUp.onVerified()
    expect(isStepUpCancelled(await pending)).toBe(true)
    expect(action).toHaveBeenCalledTimes(1)
  })

  it('does not open an old verification prompt after a delayed failure crosses login sessions', async () => {
    localStorage.setItem('auth_session_id', 'original-session')
    let reject!: (error: unknown) => void
    const stepUp = useStepUp()
    const action = vi.fn(() => new Promise<void>((_, fail) => { reject = fail }))
    const pending = stepUp.run(action).catch(error => error)
    localStorage.setItem('auth_session_id', 'replacement-session')
    reject({ code: 'STEP_UP_REQUIRED' })
    await Promise.resolve()
    await Promise.resolve()
    expect(stepUp.visible.value).toBe(false)
    expect(isStepUpCancelled(await pending)).toBe(true)
    expect(action).toHaveBeenCalledTimes(1)
  })

  it('permits a token refresh in the same login session', async () => {
    localStorage.setItem('auth_session_id', 'original-session')
    const stepUp = useStepUp()
    const action = vi.fn().mockRejectedValueOnce({ code: 'STEP_UP_REQUIRED' }).mockResolvedValue('ok')
    const pending = stepUp.run(action)
    await Promise.resolve()
    localStorage.setItem('auth_token', 'refreshed-token')
    stepUp.onVerified()
    await expect(pending).resolves.toBe('ok')
    expect(action).toHaveBeenCalledTimes(2)
  })
})
