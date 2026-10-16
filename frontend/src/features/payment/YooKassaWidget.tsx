import { useEffect } from 'react'

import { loadWidgetScript } from './loadWidgetScript'

type Props = {
  confirmationToken: string
  returnUrl: string
  /** Любая неустранимая проблема виджета: вызывающая сторона уходит на redirect. */
  onFail: (reason: string) => void
}

/**
 * Виджет оплаты ЮKassa на своём домене (checkout-widget v1). Скрипт грузится
 * лениво при монтировании, `destroy()` вызывается при размонтировании и перед
 * любым перевыпуском токена (родитель размонтирует/перемонтирует компонент с
 * новым `confirmationToken`) — без него повторный `render` в тот же контейнер
 * даёт неопределённое поведение (документация ЮKassa).
 */
const RENDER_TIMEOUT_MS = 15_000

export function YooKassaWidget({ confirmationToken, returnUrl, onFail }: Props) {
  const containerId = 'yookassa-widget'

  useEffect(() => {
    let widget: YooMoneyCheckoutWidget | null = null
    let cancelled = false
    // Форма не нарисовалась за RENDER_TIMEOUT_MS (битый токен, сеть до ЮKassa
    // режется) — это тоже сбой: без таймаута покупатель смотрел бы на пустой
    // контейнер вместо редиректа на страницу ЮKassa.
    const renderTimeout = setTimeout(() => { if (!cancelled) onFail('widget_render_timeout') }, RENDER_TIMEOUT_MS)
    loadWidgetScript()
      .then(() => {
        if (cancelled) return
        widget = new window.YooMoneyCheckoutWidget!({
          confirmation_token: confirmationToken,
          return_url: returnUrl,
          error_callback: (e) => onFail(String((e as { error?: string })?.error ?? 'widget_error')),
        })
        return widget.render(containerId)
      })
      .then(() => clearTimeout(renderTimeout))
      .catch((e: Error) => { if (!cancelled) onFail(e.message) })
    return () => { cancelled = true; clearTimeout(renderTimeout); widget?.destroy() }
  }, [confirmationToken, returnUrl, onFail])

  return <div id={containerId} />
}
