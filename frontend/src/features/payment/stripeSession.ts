/**
 * Данные сессии Stripe между шагом оплаты и отдельной страницей формы.
 *
 * client_secret приходит с сервера только в ответе на «Оплатить», поэтому
 * передаём его через sessionStorage, а не в URL: ссылка с секретом попала бы
 * в историю браузера, referer и логи прокси. Страница /pay/stripe читает
 * сессию при монтировании; перезагрузка страницы её не теряет.
 */
export type StripeSession = {
  clientSecret: string
  publishableKey: string
}

const KEY = 'mezmo:stripe:checkout'

export function saveStripeSession(session: StripeSession): void {
  try {
    sessionStorage.setItem(KEY, JSON.stringify(session))
  } catch {
    /* приватный режим — страница покажет, что форма недоступна */
  }
}

export function readStripeSession(): StripeSession | null {
  try {
    const raw = sessionStorage.getItem(KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<StripeSession>
    if (typeof parsed.clientSecret !== 'string' || !parsed.clientSecret) return null
    if (typeof parsed.publishableKey !== 'string' || !parsed.publishableKey) return null
    return { clientSecret: parsed.clientSecret, publishableKey: parsed.publishableKey }
  } catch {
    return null
  }
}

export function clearStripeSession(): void {
  try {
    sessionStorage.removeItem(KEY)
  } catch {
    /* нечего чистить */
  }
}
