/**
 * Загрузчик Stripe.js. Скрипт грузится один раз и только на шаге оплаты
 * международной ветки: на лендинге и в RU-ветке он не нужен.
 */
let loading: Promise<void> | null = null

export function loadStripeJs(): Promise<void> {
  if (typeof window === 'undefined') return Promise.reject(new Error('no_window'))
  if (window.Stripe) return Promise.resolve()
  if (loading) return loading
  loading = new Promise((resolve, reject) => {
    const existing = document.querySelector<HTMLScriptElement>('script[data-stripe-js]')
    if (existing) {
      existing.addEventListener('load', () => resolve(), { once: true })
      existing.addEventListener('error', () => reject(new Error('stripe_js_failed')), { once: true })
      return
    }
    const script = document.createElement('script')
    script.src = 'https://js.stripe.com/v3/'
    script.async = true
    script.dataset.stripeJs = '1'
    script.onload = () => resolve()
    script.onerror = () => reject(new Error('stripe_js_failed'))
    document.head.appendChild(script)
  })
  return loading
}
