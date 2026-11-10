/**
 * Данные формы UnitPay между шагом оплаты и отдельной страницей /pay/unitpay.
 *
 * widgetUrl приходит с сервера только в ответе на «Оплатить», поэтому передаём
 * его через sessionStorage, а не в URL: ссылка попала бы в историю браузера,
 * referer и логи прокси. Страница /pay/unitpay читает сессию при монтировании.
 */
export type UnitpaySession = {
  widgetUrl: string
  returnUrl: string
}

const KEY = 'mezmo:unitpay:checkout'

export function saveUnitpaySession(session: UnitpaySession): void {
  try {
    sessionStorage.setItem(KEY, JSON.stringify(session))
  } catch {
    /* приватный режим — страница покажет, что форма недоступна */
  }
}

export function readUnitpaySession(): UnitpaySession | null {
  try {
    const raw = sessionStorage.getItem(KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<UnitpaySession>
    if (typeof parsed.widgetUrl !== 'string' || !parsed.widgetUrl) return null
    if (typeof parsed.returnUrl !== 'string' || !parsed.returnUrl) return null
    return { widgetUrl: parsed.widgetUrl, returnUrl: parsed.returnUrl }
  } catch {
    return null
  }
}

export function clearUnitpaySession(): void {
  try {
    sessionStorage.removeItem(KEY)
  } catch {
    /* нечего чистить */
  }
}
