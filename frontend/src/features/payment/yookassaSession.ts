/**
 * Данные платежа ЮKassa между шагом оплаты и отдельной страницей формы.
 *
 * confirmation_token приходит с сервера только в ответе на «Оплатить», поэтому
 * передаём его через sessionStorage, а не в URL: ссылка с токеном попала бы в
 * историю браузера, referer и логи прокси. Страница /pay/yookassa читает
 * сессию при монтировании; перезагрузка страницы её не теряет.
 */
export type YookassaSession = {
  confirmationToken: string
  returnUrl: string
}

const KEY = 'mezmo:yookassa:checkout'

export function saveYookassaSession(session: YookassaSession): void {
  try {
    sessionStorage.setItem(KEY, JSON.stringify(session))
  } catch {
    /* приватный режим — страница покажет, что форма недоступна */
  }
}

export function readYookassaSession(): YookassaSession | null {
  try {
    const raw = sessionStorage.getItem(KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<YookassaSession>
    if (typeof parsed.confirmationToken !== 'string' || !parsed.confirmationToken) return null
    if (typeof parsed.returnUrl !== 'string' || !parsed.returnUrl) return null
    return { confirmationToken: parsed.confirmationToken, returnUrl: parsed.returnUrl }
  } catch {
    return null
  }
}

export function clearYookassaSession(): void {
  try {
    sessionStorage.removeItem(KEY)
  } catch {
    /* нечего чистить */
  }
}
