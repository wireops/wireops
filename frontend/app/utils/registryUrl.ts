// Registry credentials accept either a full URL ("https://ghcr.io") or a
// bare host[:port] ("ghcr.io", "registry.example.com:5000", "localhost:5000")
// — mirroring the backend's internal/registry.NormalizeRegistryHost, which
// prepends "//" to schemeless input so the URL parser treats it as an
// authority instead of a bare relative path. Shared here so both the modal's
// submit-time validation and its "Test Connection" gate use the same rule.
const SCHEME_RE = /^[a-z][a-z0-9+.-]*:\/\//i

// Relative-path prefixes ("/etc/passwd", "./foo", "../foo") are not valid
// registry hosts — without this check the "//" prefix trick below makes the
// URL parser treat their first path segment as a hostname (e.g. "/etc/passwd"
// -> hostname "etc"), which would wrongly validate as a registry URL.
const RELATIVE_PATH_RE = /^\.{0,2}\//

export function isValidRegistryUrl(value: string): boolean {
  const trimmed = value.trim()
  if (!trimmed || /\s/.test(trimmed)) return false
  if (RELATIVE_PATH_RE.test(trimmed)) return false

  const parseable = SCHEME_RE.test(trimmed) ? trimmed : `//${trimmed}`
  let url: URL
  try {
    url = new URL(parseable, 'http://placeholder.invalid')
  } catch {
    return false
  }

  return url.hostname.length > 0
}
