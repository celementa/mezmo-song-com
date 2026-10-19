/**
 * Отметка «апсейл после оплаты уже показывали» (шаги 11–12 варианта 1).
 *
 * Живёт в localStorage: серверного поля под «апсейл предложен» нет, а плодить
 * его ради экрана, который человек видит один раз, незачем. Потеря отметки
 * (другой браузер, приватный режим, чистка данных) стоит одного лишнего показа —
 * поэтому все обращения обёрнуты в try/catch и «не знаю» читается как «не
 * показывали».
 *
 * Отдельный модуль от компонентов: react-refresh требует, чтобы файл с
 * компонентами экспортировал только компоненты.
 */
const SEEN_PREFIX = 'flow_upsell_seen:'

export function markUpsellSeen(orderRef: string) {
  try {
    localStorage.setItem(SEEN_PREFIX + orderRef, '1')
  } catch {
    // Хранилище недоступно — экран покажется ещё раз, это допустимо.
  }
}

export function upsellAlreadySeen(orderRef: string): boolean {
  try {
    return localStorage.getItem(SEEN_PREFIX + orderRef) === '1'
  } catch {
    return false
  }
}
