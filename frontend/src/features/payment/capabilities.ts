/**
 * Что умеет провайдер платёжной ветки.
 *
 * Подписка — это рекуррентное списание: первый платёж привязывает карту
 * (ЮKassa — save_payment_method, Stripe — Checkout в режиме subscription).
 * UnitPay рекуррентку пока не включил, и ручки оформления подписки отвечают
 * 503 subscriptions_unavailable. Значит, предлагать подписку в ветке UnitPay
 * нельзя: человек выбрал бы план, который невозможно оплатить.
 */
export function canSellSubscription(provider: string | null | undefined): boolean {
  return provider === 'yookassa' || provider === 'stripe'
}
