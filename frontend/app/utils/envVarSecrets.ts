// vault/infisical "value" is just a reference to where the secret lives
// (e.g. "mount/data/path#field") — not the secret itself — so it isn't
// sensitive and shouldn't be masked/hidden like an internal-provider secret.
export function isInternalSecret(env: any): boolean {
  return env.secret && (!env.secret_provider || env.secret_provider === 'internal')
}
