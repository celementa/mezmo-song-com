import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { apiGet } from '@/lib/http'
import { runOrderPurchaseTracking, type PurchaseTrackingOrderStatus } from '@/features/analytics/purchaseTracking'

// Сколько раз и с каким шагом переспрашиваем статус, пока вебхук ЮKassa
// догоняет возврат покупателя (обычно приходит в течение пары секунд).
const POLL_ATTEMPTS = 8
const POLL_INTERVAL_MS = 2000

type PageState = 'checking' | 'paid' | 'waiting' | 'notfound'

/**
 * «Спасибо за оплату» — return_url ЮKassa и одна из двух точек браузерного
 * трекинга покупки (вторая — страница заказа, см. runOrderPurchaseTracking).
 * Анти-фрод: страница не верит URL — событие уходит только после того, как
 * сервер подтвердил «оплачен», а атомарная заявка mark-purchase-tracked гасит
 * повторные открытия, прямые заходы и вторую точку.
 */
export function ThanksRoute() {
  const [searchParams] = useSearchParams()
  const orderId = searchParams.get('orderId')
  const [state, setState] = useState<PageState>(() => (orderId ? 'checking' : 'notfound'))
  const trackingFired = useRef(false)

  useEffect(() => {
    if (!orderId) return
    let cancelled = false

    const check = async (attempt: number) => {
      let st: PurchaseTrackingOrderStatus
      try {
        st = await apiGet<PurchaseTrackingOrderStatus>(`/api/public/order/${encodeURIComponent(orderId)}/status`)
      } catch {
        if (cancelled) return
        setState('notfound')
        return
      }
      if (cancelled) return

      const paid = st.paymentStatus !== 'pending' && st.status !== 'created'
      if (paid) {
        setState('paid')
        if (!trackingFired.current) {
          trackingFired.current = true
          if (!cancelled) void runOrderPurchaseTracking(st)
        }
        return
      }
      if (attempt < POLL_ATTEMPTS) {
        setTimeout(() => { void check(attempt + 1) }, POLL_INTERVAL_MS)
      } else {
        setState('waiting')
      }
    }

    void check(1)
    return () => { cancelled = true }
  }, [orderId])

  return (
    <div className="relative flex min-h-screen flex-col items-center justify-center overflow-hidden bg-[#FDFBF7] px-6 text-center">
      <style>{`
        @keyframes thanks-rise { from { opacity: 0; transform: translateY(14px); } to { opacity: 1; transform: none; } }
        @keyframes thanks-pop { 0% { transform: scale(0.4); opacity: 0; } 70% { transform: scale(1.08); } 100% { transform: scale(1); opacity: 1; } }
        @keyframes thanks-eq { 0%, 100% { transform: scaleY(0.35); } 50% { transform: scaleY(1); } }
        @keyframes thanks-glow { 0%, 100% { opacity: 0.5; } 50% { opacity: 0.9; } }
        .thanks-rise { opacity: 0; animation: thanks-rise 0.6s cubic-bezier(0.22, 1, 0.36, 1) forwards; }
        .thanks-pop { opacity: 0; animation: thanks-pop 0.5s cubic-bezier(0.34, 1.56, 0.64, 1) forwards; }
        .thanks-eq-bar { transform-origin: bottom; animation: thanks-eq 1.1s ease-in-out infinite; }
        @media (prefers-reduced-motion: reduce) {
          .thanks-rise, .thanks-pop { animation-duration: 0.01s; }
          .thanks-eq-bar { animation: none; transform: scaleY(0.7); }
        }
      `}</style>

      {/* мягкое тёплое свечение фона */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{
          background:
            'radial-gradient(ellipse 60% 45% at 50% 32%, rgba(212,175,55,0.14), transparent 70%), radial-gradient(ellipse 45% 35% at 50% 100%, rgba(225,29,72,0.06), transparent 70%)',
          animation: 'thanks-glow 5s ease-in-out infinite',
        }}
      />

      <div className="relative w-full max-w-md">
        {state === 'checking' && (
          <div className="thanks-rise">
            <div className="mx-auto h-10 w-10 animate-spin rounded-full border-4 border-[#EFE7D6] border-t-[#B5952F]" />
            <p className="mt-6 text-sm font-semibold uppercase tracking-[0.14em] text-[#B5952F]">
              Проверяем оплату
            </p>
          </div>
        )}

        {state === 'paid' && (
          <>
            {/* эквалайзер в медальоне — песня уже «зазвучала» */}
            <div className="thanks-pop mx-auto flex h-24 w-24 items-end justify-center gap-[5px] rounded-full bg-white pb-7 pt-7 shadow-[0_8px_40px_rgba(181,149,47,0.25)] ring-1 ring-[#EFE7D6]">
              {[0.9, 0.55, 1, 0.4, 0.75].map((h, i) => (
                <span
                  key={i}
                  className="thanks-eq-bar w-[6px] rounded-full bg-gradient-to-t from-[#B5952F] to-[#E11D48]"
                  style={{ height: `${h * 40}px`, animationDelay: `${i * 0.13}s` }}
                />
              ))}
            </div>

            <p className="thanks-rise mt-8 text-xs font-bold uppercase tracking-[0.18em] text-[#B5952F]" style={{ animationDelay: '0.15s' }}>
              Оплата прошла
            </p>
            <h1 className="thanks-rise mt-3 text-3xl font-bold leading-tight text-gray-900 sm:text-4xl" style={{ animationDelay: '0.25s' }}>
              Спасибо! Ваша песня уже пишется
            </h1>
            <p className="thanks-rise mt-4 text-base leading-relaxed text-gray-600" style={{ animationDelay: '0.35s' }}>
              Мы приняли заказ и передали вашу историю в работу. Ссылка на готовую
              песню придёт на почту — а следить за процессом можно прямо сейчас.
            </p>

            <div className="thanks-rise mt-8" style={{ animationDelay: '0.45s' }}>
              <Link
                to={`/order/${encodeURIComponent(orderId ?? '')}`}
                className="inline-block rounded-xl bg-[#E11D48] px-8 py-3.5 text-base font-semibold text-white shadow-[0_6px_20px_rgba(225,29,72,0.3)] transition-transform hover:scale-[1.02] hover:opacity-95"
              >
                Открыть страницу заказа
              </Link>
              <p className="mt-4 text-xs text-gray-400">
                Страница заказа сохранится в письме — её можно открыть позже с любого устройства.
              </p>
            </div>
          </>
        )}

        {state === 'waiting' && (
          <div className="thanks-rise">
            <div className="mx-auto h-10 w-10 animate-spin rounded-full border-4 border-[#EFE7D6] border-t-[#B5952F]" />
            <h1 className="mt-6 text-2xl font-bold text-gray-900">Ждём подтверждение оплаты</h1>
            <p className="mt-3 text-sm leading-relaxed text-gray-600">
              Банк ещё не подтвердил платёж — обычно это занимает меньше минуты.
              Страница заказа уже доступна, подтверждение появится там автоматически.
            </p>
            {orderId && (
              <Link
                to={`/order/${encodeURIComponent(orderId)}`}
                className="mt-6 inline-block rounded-xl border border-[#E11D48] px-6 py-3 text-sm font-semibold text-[#E11D48] transition-colors hover:bg-[#E11D48] hover:text-white"
              >
                Перейти к заказу
              </Link>
            )}
          </div>
        )}

        {state === 'notfound' && (
          <div className="thanks-rise">
            <h1 className="text-2xl font-bold text-gray-900">Заказ не найден</h1>
            <p className="mt-3 text-sm text-gray-600">
              Проверьте ссылку из письма — или напишите нам, мы поможем.
            </p>
            <Link
              to="/"
              className="mt-6 inline-block rounded-xl bg-[#E11D48] px-6 py-3 text-sm font-semibold text-white hover:opacity-95"
            >
              На главную
            </Link>
          </div>
        )}
      </div>
    </div>
  )
}
