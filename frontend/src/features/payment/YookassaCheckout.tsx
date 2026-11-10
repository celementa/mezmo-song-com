/**
 * Виджет ЮKassa (карта, СБП, SberPay, ЮMoney) на отдельной странице
 * /pay/yookassa. Раньше он монтировался внизу шага оплаты и полагался на
 * scrollIntoView — у части пользователей автоскролл не срабатывал, и форма
 * оставалась за пределами экрана. Теперь на странице только форма.
 *
 * Засчитывает оплату только уведомление ЮKassa (или сверка), 'success' лишь
 * уводит на return_url, который задал сервер.
 */
import { useEffect, useRef, useState } from 'react'

import { loadYookassaWidget } from './loadYookassaWidget'

export function YookassaCheckout({
  confirmationToken,
  returnUrl,
  onError,
}: {
  confirmationToken: string
  returnUrl: string
  onError: () => void
}) {
  // Виджет рендерится по id контейнера, а не по ссылке на узел.
  const [containerId] = useState(() => `yookassa-widget-${Math.random().toString(36).slice(2)}`)
  const onErrorRef = useRef(onError)
  useEffect(() => {
    onErrorRef.current = onError
  }, [onError])
  useEffect(() => {
    let alive = true
    let widget: { destroy: () => void } | null = null
    void loadYookassaWidget()
      .then(() => {
        const Ctor = window.YooMoneyCheckoutWidget
        if (!Ctor) throw new Error('yookassa_widget_missing')
        const instance = new Ctor({
          confirmation_token: confirmationToken,
          return_url: returnUrl,
          error_callback: () => {
            if (alive) onErrorRef.current()
          },
        })
        instance.on('success', () => window.location.assign(returnUrl))
        instance.on('fail', () => {
          if (alive) onErrorRef.current()
        })
        return instance.render(containerId).then(() => {
          if (!alive) {
            instance.destroy()
            return
          }
          widget = instance
        })
      })
      .catch(() => {
        if (alive) onErrorRef.current()
      })
    return () => {
      alive = false
      widget?.destroy()
    }
  }, [confirmationToken, returnUrl, containerId])
  return <div id={containerId} className="min-h-[420px]" />
}
