// Shared source of truth for repository_keys auth_type / oauth_provider
// values — mirrors internal/git.AuthType and internal/gitprovider slugs on
// the backend. Centralized here so the string literals can't drift between
// RepositoryKeysPanel, RepositoryCreateModal, and RepositoryKeyModal.
export const AUTH_TYPE = {
  NONE: 'none',
  SSH_KEY: 'ssh_key',
  BASIC: 'basic',
  OAUTH_TOKEN: 'oauth_token',
} as const

export type AuthType = typeof AUTH_TYPE[keyof typeof AUTH_TYPE]

export const GIT_PROVIDER = {
  GITHUB: 'github',
} as const

export type GitProviderSlug = typeof GIT_PROVIDER[keyof typeof GIT_PROVIDER]
