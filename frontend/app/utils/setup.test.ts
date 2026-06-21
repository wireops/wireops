import { describe, expect, it } from 'vitest'

import { getSetupBlockedMessage, mapSetupError, validateSetupPassword } from './setup'

describe('setup utils', () => {
  it('maps blocked reasons to friendly messages', () => {
    expect(getSetupBlockedMessage({
      needsSetup: true,
      setupAllowed: false,
      reason: 'missing_bootstrap_token',
      requiresBootstrapToken: true,
    })).toContain('BOOTSTRAP_TOKEN')

    expect(getSetupBlockedMessage({
      needsSetup: false,
      setupAllowed: false,
      reason: 'already_configured',
      requiresBootstrapToken: false,
    })).toContain('Sign in')
  })

  it('maps known setup API errors to friendly copy', () => {
    expect(mapSetupError('invalid email address')).toBe('Enter a valid email address.')
    expect(mapSetupError('password must be at least 8 characters')).toBe('Password must be at least 8 characters long.')
    expect(mapSetupError('Passwords do not match')).toBe('The password confirmation does not match.')
    expect(mapSetupError('internal error')).toContain('unexpected error')
  })

  it('validates setup password length before submit', () => {
    expect(validateSetupPassword('short')).toBe('Password must be at least 8 characters long.')
    expect(validateSetupPassword('longenough')).toBe('')
  })
})
