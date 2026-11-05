/**
 * Загрузчик виджета ЮKassa (checkout-widget v1). Скрипт грузится один раз и
 * только на шаге оплаты российской ветки: на лендинге и в других ветках он не
 * нужен. Документация:
 * https://yookassa.ru/developers/payment-acceptance/integration-scenarios/widget/integration
 */
let loading: Promise<void> | null = null

export const YOOKASSA_WIDGET_SRC = 'https://yookassa.ru/checkout-widget/v1/checkout-widget.js'

export function loadYookassaWidget(): Promise<void> {
  if (typeof window === 'undefined') return Promise.reject(new Error('no_window'))
  if (window.YooMoneyCheckoutWidget) return Promise.resolve()
  if (loading) return loading
  loading = new Promise((resolve, reject) => {
    const existing = document.querySelector<HTMLScriptElement>('script[data-yookassa-widget]')
    if (existing) {
      existing.addEventListener('load', () => resolve(), { once: true })
      existing.addEventListener('error', () => reject(new Error('yookassa_widget_failed')), { once: true })
      return
    }
    const script = document.createElement('script')
    script.src = YOOKASSA_WIDGET_SRC
    script.async = true
    script.dataset.yookassaWidget = '1'
    script.onload = () => resolve()
    script.onerror = () => reject(new Error('yookassa_widget_failed'))
    document.head.appendChild(script)
  })
  return loading
}
