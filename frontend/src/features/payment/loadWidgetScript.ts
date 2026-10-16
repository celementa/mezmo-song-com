// Скрипт виджета грузим один раз и лениво — по нажатию «Оплатить».
// Таймаут обязателен: у части пользователей домен ЮKassa режется адблоком или
// провайдером, и без таймаута кнопка «Оплатить» просто зависнет.
const SRC = 'https://yookassa.ru/checkout-widget/v1/checkout-widget.js'
const TIMEOUT_MS = 10_000

let pending: Promise<void> | null = null

export function loadWidgetScript(): Promise<void> {
  if (window.YooMoneyCheckoutWidget) return Promise.resolve()
  if (pending) return pending
  pending = new Promise<void>((resolve, reject) => {
    const s = document.createElement('script')
    s.src = SRC
    s.async = true
    const t = setTimeout(() => { reject(new Error('widget_script_timeout')) }, TIMEOUT_MS)
    s.onload = () => {
      clearTimeout(t)
      if (window.YooMoneyCheckoutWidget) resolve()
      else reject(new Error('widget_script_broken'))
    }
    s.onerror = () => { clearTimeout(t); reject(new Error('widget_script_error')) }
    document.head.appendChild(s)
  }).catch((e) => { pending = null; throw e })
  return pending
}
