/**
 * Валюта показа цен. Базовая — доллары: цены в настройках лендинга
 * (intl/settings.json) заданы в USD. Для покупателей из России банк просит
 * дублировать цены в рублях, поэтому на сайте есть переключатель «$ / ₽»;
 * выбор помнится в браузере.
 *
 * Валюта читается один раз при загрузке страницы, а смена перезагружает её:
 * цены считаются в десятке мест (в том числе в мемоизированных данных
 * лендинга), и перезагрузка проще и надёжнее подписки каждого из них.
 */
export type Currency = 'USD' | 'RUB'

/** Курс ЦБ РФ, ₽ за $1 (октябрь 2026). Рублёвые цены считаются от него. */
export const RUB_PER_USD = 85.5

const KEY = 'mezmo:currency'

function readCurrency(): Currency {
  try {
    return localStorage.getItem(KEY) === 'RUB' ? 'RUB' : 'USD'
  } catch {
    return 'USD'
  }
}

export const CURRENCY: Currency = typeof window === 'undefined' ? 'USD' : readCurrency()

export function setCurrency(next: Currency) {
  if (next === CURRENCY) return
  try {
    localStorage.setItem(KEY, next)
  } catch {
    // без localStorage выбор не запомнится — показываем доллары
  }
  window.location.reload()
}

/** Сумма в валюте показа: доллары как есть, рубли — по курсу с округлением до 10 ₽. */
export function inCurrency(usd: number): number {
  if (CURRENCY === 'USD') return usd
  return Math.round((usd * RUB_PER_USD) / 10) * 10
}
