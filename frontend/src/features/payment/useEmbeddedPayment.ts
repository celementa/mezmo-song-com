/**
 * Оплата виджетом ЮKassa на своём домене с фолбэком на редирект.
 *
 * Шаг оплаты монтируется → сразу просим у сервера embedded-платёж и рисуем
 * виджет (карта, СБП, SberPay, T-Pay — что включено у магазина). После
 * успешной оплаты виджет сам уводит на returnUrl, который сервер отдал вместе
 * с токеном: правила возврата живут на бэкенде, фронт их не дублирует.
 *
 * Виджет не загрузился (адблок, таймаут, битый токен) — тот же платёж
 * запрашивается в режиме redirect, и покупатель уходит на страницу ЮKassa:
 * потерять удобство лучше, чем продажу. Мок-сервер отдаёт только
 * confirmationUrl, поэтому на стенде всегда работает ветка редиректа.
 */

import { useCallback, useEffect, useRef, useState } from 'react'

export type PaymentMode = 'embedded' | 'redirect'

/** Ответ любой ручки, заводящей платёж (initiate-payment, subscribe-instead, subscription/start). */
export type PaymentStart = {
  confirmationToken?: string
  returnUrl?: string
  confirmationUrl?: string
  /** Оплата не нужна: цена 0, dev-режим, подписка уже активна. */
  paid?: boolean
  activated?: boolean
}

export type EmbeddedPaymentState =
  | { status: 'starting' }
  | { status: 'widget'; token: string; returnUrl: string }
  | { status: 'redirecting' }
  | { status: 'error'; message: string }

const SETTLED_ERRORS = ['already_paid', 'not_pending']

export function useEmbeddedPayment({
  start,
  onPaid,
  errorText = 'Не получилось перейти к оплате. Попробуйте ещё раз.',
}: {
  /** Заводит платёж в нужном режиме; бросает Error с кодом сервера. */
  start: (mode: PaymentMode) => Promise<PaymentStart>
  /** Платить нечего или заказ уже оплачен — идём дальше. */
  onPaid: () => void
  errorText?: string
}) {
  const [state, setState] = useState<EmbeddedPaymentState>({ status: 'starting' })

  const begin = useCallback(
    async (mode: PaymentMode) => {
      setState({ status: 'starting' })
      try {
        const res = await start(mode)
        if (res.confirmationToken && res.returnUrl && mode === 'embedded') {
          setState({ status: 'widget', token: res.confirmationToken, returnUrl: res.returnUrl })
          return
        }
        if (res.confirmationUrl) {
          setState({ status: 'redirecting' })
          window.location.assign(res.confirmationUrl)
          return
        }
        if (res.paid || res.activated) {
          onPaid()
          return
        }
        // Ни токена, ни ссылки, ни «оплачено» — сервер ответил не по контракту.
        setState({ status: 'error', message: errorText })
      } catch (e) {
        const msg = e instanceof Error ? e.message : ''
        if (SETTLED_ERRORS.includes(msg)) {
          onPaid()
          return
        }
        setState({ status: 'error', message: errorText })
      }
    },
    [start, onPaid, errorText],
  )

  const fired = useRef(false)
  useEffect(() => {
    if (fired.current) return
    fired.current = true
    void begin('embedded')
  }, [begin])

  /** Виджет сломался — тот же платёж редиректом. */
  const widgetFail = useCallback(
    (reason: string) => {
      console.warn('[pay] widget failed, fallback to redirect:', reason)
      void begin('redirect')
    },
    [begin],
  )

  const retry = useCallback(() => void begin('embedded'), [begin])

  return { state, widgetFail, retry }
}
