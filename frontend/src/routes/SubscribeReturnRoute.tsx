import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { apiGet, apiPost } from '@/lib/http'

// Возврат с оплаты ЮKassa: поллим статус подписки, при активации логиним по
// email (он пришёл в query с нашей же страницы оформления) и ведём в кабинет.
export function SubscribeReturnRoute() {
  const [params] = useSearchParams()
  const sid = params.get('sid') ?? ''
  const email = params.get('email') ?? ''
  // Без sid ждать нечего — сразу «не подтвердилось» (и без setState в эффекте).
  const [state, setState] = useState<'waiting' | 'active' | 'failed'>(sid ? 'waiting' : 'failed')
  const attempts = useRef(0)

  useEffect(() => {
    if (!sid) return
    let cancelled = false
    const tick = async () => {
      attempts.current += 1
      try {
        const res = await apiGet<{ active: boolean; status: string }>(
          `/api/subscription/status?sid=${encodeURIComponent(sid)}`,
        )
        if (cancelled) return
        if (res.active) {
          setState('active')
          if (email) {
            try {
              await apiPost('/api/subscription/session', { email })
            } catch {
              // Не залогинились — кабинет попросит email сам.
            }
          }
          if (!cancelled) window.location.replace('/account')
          return
        }
      } catch {
        // Сеть мигнула — просто попробуем ещё раз.
      }
      if (cancelled) return
      // Вебхук обычно приходит за секунды; через ~2 минуты перестаём ждать.
      if (attempts.current >= 40) {
        setState('failed')
        return
      }
      setTimeout(tick, 3000)
    }
    void tick()
    return () => {
      cancelled = true
    }
  }, [sid, email])

  return (
    <div
      className="flex min-h-screen flex-col items-center justify-center px-4 text-center"
      style={{ backgroundColor: '#f9f4ed', color: '#151210', fontFamily: "'Onest', sans-serif" }}
    >
      {state !== 'failed' ? (
        <>
          <div
            className="h-10 w-10 animate-spin rounded-full border-4 border-transparent"
            style={{ borderTopColor: '#B9852E', borderRightColor: '#B9852E' }}
          />
          <h1 className="mt-6 text-2xl" style={{ fontFamily: "'Prata', serif" }}>
            {state === 'active' ? 'Подписка активна!' : 'Подтверждаем оплату…'}
          </h1>
          <p className="mt-2 max-w-sm text-sm" style={{ color: '#6b6558' }}>
            {state === 'active' ? 'Открываем личный кабинет.' : 'Обычно это занимает несколько секунд.'}
          </p>
        </>
      ) : (
        <>
          <h1 className="text-2xl" style={{ fontFamily: "'Prata', serif" }}>Оплата пока не подтвердилась</h1>
          <p className="mt-2 max-w-md text-sm" style={{ color: '#6b6558' }}>
            Если деньги списались — подписка активируется в ближайшие минуты, и вы сможете войти в кабинет по
            своему email. Если оплата не прошла, попробуйте оформить подписку ещё раз.
          </p>
          <div className="mt-6 flex gap-3">
            <Link
              to="/account"
              className="rounded-xl px-6 py-3 text-sm font-black"
              style={{ backgroundColor: '#151210', color: '#F2C572' }}
            >
              В кабинет
            </Link>
            <Link
              to="/subscribe"
              className="rounded-xl border px-6 py-3 text-sm font-bold"
              style={{ borderColor: '#ddd0b8', color: '#6b6558' }}
            >
              К оформлению
            </Link>
          </div>
        </>
      )}
    </div>
  )
}
