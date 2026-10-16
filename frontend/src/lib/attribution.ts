/**
 * Ad attribution: снимает рекламные метки с посадочной страницы и хранит их в
 * localStorage, чтобы приложить к заказу при создании драфта. Без этого источник
 * покупки восстанавливается только из access-логов nginx.
 *
 * localStorage, а не sessionStorage: клик из рекламы должен «дожить» до покупки,
 * даже если человек вернулся на сайт позже и уже напрямую. Клик-айди (yclid,
 * rb_clickid) нужны для офлайн-конверсий в Директ и VK.
 */

const KEY = '_attr'

const AD_PARAMS = [
  // Персональная метка партнёра/инфлюенсера: раздаём ссылки вида `?ref=nickname`,
  // когда у площадки нет своего клик-айди и UTM-разметку проставить некому.
  'ref',
  'utm_source',
  'utm_medium',
  'utm_campaign',
  'utm_content',
  'utm_term',
  'yclid',
  'gclid',
  'rb_clickid',
  'fbclid',
] as const

export type Attribution = Record<string, string>

function cut(value: string, max: number): string {
  return value.length > max ? value.slice(0, max) : value
}

/**
 * Вызывается один раз при входе на сайт. Реклама (есть метки) перезатирает
 * сохранённое — last click wins. Заход без меток запоминается только если
 * ничего не сохранено, чтобы не терять рекламный клик на повторном визите.
 */
export function captureAttribution(): void {
  try {
    const params = new URLSearchParams(window.location.search)
    const found: Attribution = {}
    for (const key of AD_PARAMS) {
      const value = params.get(key)
      if (value) found[key] = cut(value, 500)
    }

    const isAdClick = Object.keys(found).length > 0
    if (!isAdClick && localStorage.getItem(KEY)) return

    found.landingUrl = cut(window.location.pathname + window.location.search, 1000)
    if (document.referrer) found.referrer = cut(document.referrer, 500)
    found.capturedAt = new Date().toISOString()
    localStorage.setItem(KEY, JSON.stringify(found))
  } catch {
    /* приватный режим — storage недоступен */
  }
}

export function getAttribution(): Attribution | null {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' ? (parsed as Attribution) : null
  } catch {
    return null
  }
}
