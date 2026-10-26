import { useSyncExternalStore } from 'react'

/**
 * Согласие на cookie аналитики и рекламы — то, что обещают Privacy Policy и
 * Consent (src/legal/en): баннер, ссылка «Cookie settings» в футере, Global
 * Privacy Control.
 *
 * Теги Google в GTM уважают Consent Mode v2: состояние по умолчанию ставит
 * скрипт в index.html ДО загрузки GTM (читает тот же ключ localStorage), здесь —
 * выбор человека и `gtag('consent', 'update')`. Пиксели вне GTM (MetaPixel,
 * TikTokPixel) подключаются только при согласии на рекламу (PublicRoot).
 *
 * Пока выбора нет: для посетителей из ЕС/ЕЭЗ/UK (по часовому поясу) аналитика и
 * реклама выключены до согласия; остальным включены, баннер — уведомление с
 * возможностью отказаться. GPC в браузере всегда выключает рекламу.
 */
export type ConsentChoice = { analytics: boolean; ads: boolean }

const KEY = 'mezmo:consent'
const OPEN_EVENT = 'mezmo:cookie-settings'

type ConsentEnv = { strict: boolean; gpc: boolean }

function env(): ConsentEnv {
  const w = window as unknown as { __consentEnv?: ConsentEnv }
  return w.__consentEnv ?? { strict: true, gpc: false }
}

function readChoice(): ConsentChoice | null {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) ?? 'null')
    if (raw && typeof raw.analytics === 'boolean' && typeof raw.ads === 'boolean') return raw
  } catch {
    // нет localStorage или мусор в ключе — считаем, что выбора не было
  }
  return null
}

let choice: ConsentChoice | null = typeof window === 'undefined' ? null : readChoice()
const listeners = new Set<() => void>()

function subscribe(notify: () => void) {
  listeners.add(notify)
  return () => {
    listeners.delete(notify)
  }
}

/** Выбор человека; null — баннер ещё не отвечен. */
export function useConsentChoice(): ConsentChoice | null {
  return useSyncExternalStore(subscribe, () => choice, () => null)
}

/** Действующее согласие: выбор человека или значение по умолчанию для региона. */
export function effectiveConsent(c: ConsentChoice | null = choice): ConsentChoice {
  const { strict, gpc } = env()
  const base = c ?? { analytics: !strict, ads: !strict }
  return { analytics: base.analytics, ads: base.ads && !gpc }
}

export function saveConsent(next: ConsentChoice) {
  choice = next
  try {
    localStorage.setItem(KEY, JSON.stringify(next))
  } catch {
    // без localStorage выбор действует до конца визита
  }
  const eff = effectiveConsent(next)
  const ads = eff.ads ? 'granted' : 'denied'
  const w = window as unknown as { gtag?: (...args: unknown[]) => void }
  w.gtag?.('consent', 'update', {
    analytics_storage: eff.analytics ? 'granted' : 'denied',
    ad_storage: ads,
    ad_user_data: ads,
    ad_personalization: ads,
  })
  listeners.forEach((notify) => notify())
}

/** Есть ли в браузере сигнал Global Privacy Control (реклама выключена всегда). */
export function hasGpc(): boolean {
  return env().gpc
}

/** Открыть панель настроек cookie (ссылка «Cookie settings» в футере). */
export function openCookieSettings() {
  window.dispatchEvent(new Event(OPEN_EVENT))
}

export function onOpenCookieSettings(handler: () => void) {
  window.addEventListener(OPEN_EVENT, handler)
  return () => window.removeEventListener(OPEN_EVENT, handler)
}
