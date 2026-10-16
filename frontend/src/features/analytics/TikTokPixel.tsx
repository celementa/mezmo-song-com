import { useEffect } from 'react'
import { useLocation } from 'react-router-dom'

declare global {
  interface Window {
    // Объект ttq собирается динамически (методы генерирует _initTtqStub из списка
    // ttq.methods), поэтому описываем только то, что реально вызываем из приложения.
    ttq?: {
      load: (pixelId: string) => void
      track: (eventName: string, data?: Record<string, unknown>) => void
      page: () => void
    }
    TiktokAnalyticsObject?: string
  }
}

const TIKTOK_PIXEL_SCRIPT_ID = 'tiktok-pixel-base'

let _pixelLoadScheduled = false
let _stubInitialized = false
const _pendingInits: string[] = []
const _pendingTracks: Array<[string, Record<string, unknown>?]> = []

/**
 * Set up window.ttq stub immediately so ttq.track() / ttq.page() calls work
 * right away (queued), even before the SDK script has loaded.
 * The actual SDK script injection is still delayed 3.5s to protect TBT.
 */
function _initTtqStub() {
  if (_stubInitialized || window.ttq) return
  _stubInitialized = true

  /* eslint-disable prefer-rest-params, @typescript-eslint/no-explicit-any --
     официальный загрузочный сниппет TikTok Pixel. Держим дословно, чтобы сверять с
     документацией при обновлении: методы стаба генерируются через объект arguments. */
  ;(function (w: any, d: any, t: string) {
    w.TiktokAnalyticsObject = t
    const ttq = (w[t] = w[t] || []) as any
    ttq.methods = [
      'page', 'track', 'identify', 'instances', 'debug', 'on', 'off',
      'once', 'ready', 'alias', 'group', 'enableCookie', 'disableCookie',
      'holdConsent', 'revokeConsent', 'grantConsent',
    ]
    ttq.setAndDefer = function (t: any, e: string) {
      t[e] = function () {
        t.push([e].concat(Array.prototype.slice.call(arguments, 0)))
      }
    }
    for (let i = 0; i < ttq.methods.length; i++) {
      ttq.setAndDefer(ttq, ttq.methods[i])
    }
    ttq.instance = function (t: string) {
      const e = ttq._i?.[t] || []
      for (let n = 0; n < ttq.methods.length; n++) {
        ttq.setAndDefer(e, ttq.methods[n])
      }
      return e
    }
    ttq.load = function (e: string, n?: any) {
      const r = 'https://analytics.tiktok.com/i18n/pixel/events.js'
      ttq._i = ttq._i || {}
      ttq._i[e] = []
      ttq._i[e]._u = r
      ttq._t = ttq._t || {}
      ttq._t[e] = +new Date()
      ttq._o = ttq._o || {}
      ttq._o[e] = n || {}
      const o = d.createElement('script')
      o.id = TIKTOK_PIXEL_SCRIPT_ID
      o.type = 'text/javascript'
      o.async = true
      o.src = r + '?sdkid=' + e + '&lib=' + t
      const a = d.getElementsByTagName('script')[0]
      a.parentNode.insertBefore(o, a)
    }
  })(window, document, 'ttq')
  /* eslint-enable prefer-rest-params, @typescript-eslint/no-explicit-any */
}

function _doLoadPixel() {
  // Inject the actual SDK by calling ttq.load() for each pending pixel ID.
  // The stub is already set up — this triggers the real script fetch.
  _pendingInits.forEach(id => window.ttq?.load(id))
  _pendingInits.length = 0
  _pendingTracks.forEach(([ev, data]) => window.ttq?.track(ev, data ?? {}))
  _pendingTracks.length = 0
}

export function ensureTikTokPixelLoaded(pixelId: string) {
  const id = (pixelId ?? '').trim()
  if (!id) return
  if (typeof window === 'undefined' || typeof document === 'undefined') return

  // Always init the stub immediately — ensures ttq.track() works right away
  _initTtqStub()

  if (document.getElementById(TIKTOK_PIXEL_SCRIPT_ID)) {
    // SDK already injected — just register this pixel ID
    window.ttq?.load(id)
    return
  }

  _pendingInits.push(id)
  if (!_pixelLoadScheduled) {
    _pixelLoadScheduled = true
    // Delay actual SDK script injection by 3.5s to protect TBT
    setTimeout(_doLoadPixel, 3500)
  }
}

function _trackTikTok(eventName: string, data?: Record<string, unknown>) {
  // ttq stub is set up before this is ever called, so this always queues/fires
  if (window.ttq) {
    window.ttq.track(eventName, data ?? {})
  } else {
    _pendingTracks.push([eventName, data])
  }
}

export function TikTokPixel({ pixelId }: { pixelId: string }) {
  const location = useLocation()

  useEffect(() => {
    ensureTikTokPixelLoaded(pixelId)
    window.ttq?.page()
    // Only fire PageView on actual route (pathname) changes.
    // Query param changes (e.g. ?step=1 → ?step=2) are funnel steps,
    // not new page views — excluding location.search prevents duplicate
    // PageViews as the user progresses through the config steps.
  }, [pixelId, location.pathname])

  return null
}

/**
 * Fire a TikTok pixel event.
 */
export function trackTikTokEvent(eventName: string, data?: Record<string, unknown>) {
  _trackTikTok(eventName, data)
}

/**
 * Execute an admin-provided TikTok pixel script string.
 * Pass vars to inject named variables into the script scope.
 * window.ttq is always available (stub or SDK) when this is called.
 */
export function executeTikTokScript(script: string | null | undefined, vars?: Record<string, unknown>) {
  if (!script || typeof script !== 'string') return
  const cleaned = script.replace(/<\/?script[^>]*>/gi, '').trim()
  if (!cleaned) return
  try {
    // Ensure ttq stub is available for scripts that reference it
    _initTtqStub()
    if (vars && Object.keys(vars).length > 0) {
      const names = Object.keys(vars)
      const values = names.map(k => vars[k])
      new Function(...names, cleaned)(...values)
    } else {
      new Function(cleaned)()
    }
  } catch (e) {
    console.warn('[TikTokPixel] Script execution error:', e)
  }
}
