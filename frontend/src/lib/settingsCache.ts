import { apiGet } from './http'
import type { Loose } from './loose'

const _cache = new Map<string, { promise: Promise<unknown>; ts: number }>()
const TTL = 60_000 // match server Cache-Control max-age=60

/**
 * Returns public settings, using server-inlined data when available.
 * The server injects window.__SETTINGS__ into index.html at startup,
 * so the default homepage settings are available instantly (no API call).
 * Theme-specific or split-test settings still hit the API.
 */
export function cachedSettingsGet<T>(url: string): Promise<T> {
  const now = Date.now()
  const hit = _cache.get(url)
  if (hit && now - hit.ts < TTL) return hit.promise as Promise<T>

  // Use inlined settings when available. The server inlines the default theme's
  // settings, so we can also match ?theme=<defaultSlug> requests.
  const inlined = (window as unknown as Loose).__SETTINGS__
  if (inlined) {
    const bare = url === '/api/public/settings' || url === '/api/public/settings?'
    const defaultSlug = inlined.defaultThemeSlug
    const themedDefault = defaultSlug && url === `/api/public/settings?theme=${encodeURIComponent(defaultSlug)}`
    if (bare || themedDefault) {
      const promise = Promise.resolve(inlined as T)
      _cache.set(url, { promise, ts: now })
      return promise
    }
  }

  const promise = apiGet<T>(url).catch((err) => {
    _cache.delete(url)
    return Promise.reject(err)
  })
  _cache.set(url, { promise, ts: now })
  return promise
}
