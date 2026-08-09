// Registry credentials accept either a full URL ("https://ghcr.io") or a
// bare host[:port] ("ghcr.io", "registry.example.com:5000", "localhost:5000")
// — mirroring the backend's internal/registry.NormalizeRegistryHost, which
// prepends "//" to schemeless input so the URL parser treats it as an
// authority instead of a bare relative path. Shared here so both the modal's
// submit-time validation and its "Test Connection" gate use the same rule.
const SCHEME_RE = /^[a-z][a-z0-9+.-]*:\/\//i

export function isValidRegistryUrl(value: string): boolean {
  const trimmed = value.trim()
  if (!trimmed || /\s/.test(trimmed)) return false

  const parseable = SCHEME_RE.test(trimmed) ? trimmed : `//${trimmed}`
  let url: URL
  try {
    url = new URL(parseable, 'http://placeholder.invalid')
  } catch {
    return false
  }

  return url.hostname.length > 0
}
