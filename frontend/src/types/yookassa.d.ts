// Виджет ЮKassa (checkout-widget v1). Конструктор и методы — по документации:
// https://yookassa.ru/developers/payment-acceptance/integration-scenarios/widget/integration
type YooWidgetOptions = {
  confirmation_token: string
  return_url: string
  error_callback?: (error: unknown) => void
}
declare class YooMoneyCheckoutWidget {
  constructor(options: YooWidgetOptions)
  render(containerId: string): Promise<void>
  on(event: string, cb: (payload?: unknown) => void): void
  destroy(): void
}
interface Window {
  YooMoneyCheckoutWidget?: typeof YooMoneyCheckoutWidget
}
