import type { SetupStatus } from '~/types/setup'

const MIN_SETUP_PASSWORD_LENGTH = 8

export function getSetupBlockedMessage(status: SetupStatus | null | undefined) {
  switch (status?.reason) {
    case 'missing_bootstrap_token':
      return 'Initial setup is blocked because BOOTSTRAP_TOKEN is not configured on the server. Add the token to the Wireops server environment and reload this page.'
    case 'already_configured':
      return 'Setup has already been completed for this Wireops instance. Sign in with the existing administrator account to continue.'
    case 'unknown':
      return 'Wireops could not determine whether setup is available right now. Refresh and try again.'
    default:
      return ''
  }
}

export function mapSetupError(message?: string) {
  switch (message) {
    case 'email, password and bootstrapToken are required':
      return 'Fill in the email, password, and bootstrap token before continuing.'
    case 'invalid email address':
      return 'Enter a valid email address.'
    case 'password must be at least 8 characters':
      return 'Password must be at least 8 characters long.'
    case 'invalid bootstrap token':
      return 'The bootstrap token is invalid. Check the server configuration and try again.'
    case 'bootstrap token is not configured':
      return 'BOOTSTRAP_TOKEN is not configured on the server.'
    case 'setup has already been completed':
      return 'Setup has already been completed. Sign in with the existing administrator account.'
    case 'internal error':
      return 'Wireops hit an unexpected error while creating the administrator account. Check the server logs and try again.'
    case 'Passwords do not match':
      return 'The password confirmation does not match.'
    default:
      return message || 'Setup failed'
  }
}

export function validateSetupPassword(password: string) {
  if (password.length < MIN_SETUP_PASSWORD_LENGTH) {
    return mapSetupError('password must be at least 8 characters')
  }

  return ''
}
