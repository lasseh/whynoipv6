// Accept pasted URLs (http://vg.no/, https://www.vg.no/path?q=1): anything
// URL-shaped reduces to its hostname; ports, paths, and queries drop. Plain
// hosts pass through, and unparsable input goes to the API's validator.
export function extractHost(raw: string): string {
  const trimmed = raw.trim()
  if (!trimmed) return ''
  try {
    const url = /^[a-z][a-z0-9+.-]*:\/\//i.test(trimmed)
      ? new URL(trimmed)
      : new URL(`http://${trimmed}`)
    return url.hostname
  } catch {
    return trimmed
  }
}
