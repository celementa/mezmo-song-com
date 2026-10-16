import { useSyncExternalStore } from 'react'

/**
 * Валюта показа цен. Базовая — доллары: цены в настройках лендинга
 * (intl/settings.json) заданы в USD. Для покупателей из России банк просит
 * дублировать цены в рублях, поэтому на сайте есть переключатель «$ / ₽»;
 * выбор помнится в браузере и применяется сразу, без перезагрузки:
 * компоненты с ценами подписаны на валюту через useCurrency.
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

let current: Currency = typeof window === 'undefined' ? 'USD' : readCurrency()
const listeners = new Set<() => void>()

export function getCurrency(): Currency {
  return current
}

export function setCurrency(next: Currency) {
  if (next === current) return
  current = next
  try {
    localStorage.setItem(KEY, next)
  } catch {
    // без localStorage выбор не запомнится между визитами — на странице он всё равно применится
  }
  listeners.forEach((notify) => notify())
}

function subscribe(notify: () => void) {
  listeners.add(notify)
  return () => {
    listeners.delete(notify)
  }
}

/** Текущая валюта; компонент перерисуется при переключении. */
export function useCurrency(): Currency {
  return useSyncExternalStore(subscribe, getCurrency, () => 'USD')
}

/** Сумма в валюте показа: доллары как есть, рубли — по курсу с округлением до 10 ₽. */
export function inCurrency(usd: number, currency: Currency = current): number {
  if (currency === 'USD') return usd
  return Math.round((usd * RUB_PER_USD) / 10) * 10
}
