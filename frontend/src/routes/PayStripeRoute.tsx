/**
 * Отдельная страница формы Stripe. Нужна, чтобы на экране была только форма
 * оплаты: во флоу виджет монтировался посреди сводки и юридического текста, и
 * человеку приходилось прокручивать к нему. Сюда переходим кнопкой «Оплатить»
 * после того, как сервер завёл сессию Stripe; client_secret берём из
 * sessionStorage (см. features/payment/stripeSession.ts), в URL его не кладём.
 */
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { StripeEmbeddedCheckout } from '@/features/payment/StripeCheckout'
import { clearStripeSession, readStripeSession, type StripeSession } from '@/features/payment/stripeSession'
import { localePath, useTr } from '@/lib/lang'

export function PayStripeRoute() {
  const tr = useTr()
  const [session] = useState<StripeSession | null>(() => readStripeSession())
  const [failed, setFailed] = useState(false)

  // Сессию прочитали один раз при маунте; при уходе со страницы чистим, чтобы
  // следующая покупка не открыла форму прошлого заказа.
  useEffect(() => {
    return () => clearStripeSession()
  }, [])

  return (
    <div className="min-h-screen bg-[#FBF7F1]">
      <header className="flex items-center justify-between px-4 py-4 md:px-10">
        <Link to={localePath('/')} className="inline-flex items-center">
          <img src="/images/logo-mezmo.png" alt="Mezmo" className="h-5 w-auto md:h-6" />
        </Link>
        <button
          type="button"
          onClick={() => window.history.back()}
          className="text-sm font-semibold text-[#6b6558] transition hover:text-[#151210]"
        >
          ← {tr('Назад', 'Back')}
        </button>
      </header>

      <main className="mx-auto w-full max-w-lg px-4 pb-16 pt-2">
        {failed || !session ? (
          <div className="rounded-2xl border border-[#EFE4D2] bg-white p-5 text-sm text-[#3b352f]">
            <p className="font-bold">
              {tr('Форма оплаты недоступна', 'The payment form is unavailable')}
            </p>
            <p className="mt-2 text-[#6b6259]">
              {tr(
                'Откройте заказ ещё раз и нажмите «Оплатить» — форма откроется заново.',
                'Open the order again and press “Pay” — the form will open anew.',
              )}
            </p>
            <button
              type="button"
              onClick={() => window.history.back()}
              className="mt-4 rounded-xl bg-[#151210] px-5 py-2.5 text-sm font-bold text-white"
            >
              {tr('Вернуться к заказу', 'Back to the order')}
            </button>
          </div>
        ) : (
          <StripeEmbeddedCheckout
            clientSecret={session.clientSecret}
            publishableKey={session.publishableKey}
            onError={() => {
              setFailed(true)
              clearStripeSession()
            }}
          />
        )}
      </main>
    </div>
  )
}
