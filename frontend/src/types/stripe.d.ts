/** Минимальные типы Stripe.js для встроенной формы Checkout. */

export type StripeEmbeddedCheckout = {
  mount: (element: HTMLElement | string) => void
  destroy: () => void
}

export type StripeInstance = {
  initEmbeddedCheckout: (options: { clientSecret: string }) => Promise<StripeEmbeddedCheckout>
}

declare global {
  interface Window {
    Stripe?: (publishableKey: string) => StripeInstance
  }
}

export {}
