/**
 * Форма UnitPay на отдельной странице /pay/unitpay: их widget во iframe на весь
 * экран. Делает то же, что их widget.js, но без него: тот вешает глобальный
 * window.onmessage и не проверяет, от кого пришло сообщение. Форма шлёт
 * родителю JSON-строки {action: 'success' | 'close' | ...}.
 *
 * Оплату засчитывает только уведомление UnitPay серверу — 'success' лишь уводит
 * на returnUrl, который задал сервер. 'close' возвращает к шагу оплаты.
 */
import { useEffect, useRef } from 'react'

import { useTr } from '@/lib/lang'

const WIDGET_ORIGIN = 'https://widget.unitpay.money'

export function UnitpayCheckoutFrame({
  widgetUrl,
  returnUrl,
  onClose,
}: {
  widgetUrl: string
  returnUrl: string
  onClose: () => void
}) {
  const tr = useTr()
  const onCloseRef = useRef(onClose)
  useEffect(() => {
    onCloseRef.current = onClose
  }, [onClose])
  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if (e.origin !== WIDGET_ORIGIN || typeof e.data !== 'string') return
      let action: unknown
      try {
        action = (JSON.parse(e.data) as { action?: unknown }).action
      } catch {
        return
      }
      if (action === 'success') window.location.assign(returnUrl)
      if (action === 'close') onCloseRef.current()
    }
    window.addEventListener('message', onMessage)
    document.body.classList.add('overflow-hidden')
    return () => {
      window.removeEventListener('message', onMessage)
      document.body.classList.remove('overflow-hidden')
    }
  }, [returnUrl])

  return (
    <div className="fixed inset-0 z-[99999] bg-black/60">
      <iframe src={widgetUrl} title={tr('Оплата UnitPay', 'UnitPay payment')} allow="payment" className="h-full w-full border-0 bg-transparent" />
    </div>
  )
}
