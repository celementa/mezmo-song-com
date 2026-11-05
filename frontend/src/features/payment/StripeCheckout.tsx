/**
 * Встроенная форма Stripe (Embedded Checkout) в том виде, в каком её ставит
 * страница /pay/stripe: на экране только она. Карты, Apple Pay и Google Pay
 * показывает сама форма провайдера.
 */
import { useEffect, useRef } from 'react'

import { loadStripeJs } from './loadStripe'

export function StripeEmbeddedCheckout({
  clientSecret,
  publishableKey,
  onError,
}: {
  clientSecret: string
  publishableKey: string
  onError: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  // Колбэк ошибки не должен пересоздавать форму: родитель передаёт inline-функцию,
  // её идентичность меняется на каждом рендере, и useEffect с onError в
  // зависимостях уничтожал бы и монтировал Stripe заново.
  const onErrorRef = useRef(onError)
  useEffect(() => {
    onErrorRef.current = onError
  }, [onError])
  useEffect(() => {
    let alive = true
    let instance: { destroy: () => void } | null = null
    void loadStripeJs()
      .then(() => {
        const factory = window.Stripe
        if (!factory) throw new Error('stripe_js_missing')
        return factory(publishableKey).initEmbeddedCheckout({ clientSecret })
      })
      .then((checkout) => {
        if (!alive || !ref.current) {
          checkout.destroy()
          return
        }
        checkout.mount(ref.current)
        instance = checkout
      })
      .catch(() => {
        if (alive) onErrorRef.current()
      })
    return () => {
      alive = false
      instance?.destroy()
    }
  }, [clientSecret, publishableKey])
  return <div ref={ref} className="min-h-[420px]" />
}
