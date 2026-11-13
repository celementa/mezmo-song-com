import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { apiGet, apiPost } from '@/lib/http'
import { localePath } from '@/lib/lang'
import { cream, gold, goldDeep, ink, muted, serif } from '@/features/subscription/styles'

// Страница только русская (в /en маршрута нет), поэтому строки — русскими
// литералами, без tr(), а ссылки — с явным 'ru': человек остаётся в русской
// версии сайта, даже если кука выбора языка говорила обратное.
const RU = 'ru'

// Вебхук ЮKassa обычно приходит за секунды. 40 попыток по 3 секунды — примерно
// две минуты: дольше спиннер держать нельзя, человек решит, что страница повисла.
const POLL_INTERVAL_MS = 3000
const MAX_ATTEMPTS = 40

const bodyFont = "'Onest', sans-serif"

type PayState = 'waiting' | 'active' | 'failed'

// Возврат с оплаты ЮKassa: поллим статус подписки, при активации логиним по
// email (он пришёл в query с нашей же страницы оформления) и ведём в кабинет.
export function SubscribeReturnRoute() {
  const [params] = useSearchParams()
  const sid = params.get('sid') ?? ''
  const email = params.get('email') ?? ''
  // Без sid ждать нечего — сразу «не подтвердилось» (и без setState в эффекте).
  const [state, setState] = useState<PayState>(sid ? 'waiting' : 'failed')
  const attempts = useRef(0)

  useEffect(() => {
    if (!sid) return
    // Счётчик принадлежит текущему sid: новый sid — новый отсчёт, иначе
    // навигация с одного возврата на другой сразу показала бы «не подтвердилось».
    attempts.current = 0
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | null = null

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
            // Не залогинились — кабинет попросит email сам, редирект всё равно нужен.
            await apiPost('/api/subscription/session', { email }).catch(() => {})
          }
          if (cancelled) return
          window.location.replace(localePath('/account', RU))
          return
        }
      } catch {
        // Сеть мигнула — это не повод перестать ждать: вебхук может быть в пути.
      }
      if (cancelled) return
      if (attempts.current >= MAX_ATTEMPTS) {
        setState('failed')
        return
      }
      timer = setTimeout(() => void tick(), POLL_INTERVAL_MS)
    }

    void tick()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [sid, email])

  return (
    <div
      className="flex min-h-screen flex-col items-center justify-center px-4 text-center"
      style={{ backgroundColor: cream, color: ink, fontFamily: bodyFont }}
    >
      {state === 'failed' ? (
        <>
          <h1 className="text-2xl" style={serif}>
            Оплата пока не подтвердилась
          </h1>
          <p className="mt-2 max-w-md text-sm" style={{ color: muted }}>
            Если деньги списались — подписка активируется в ближайшие минуты, и вы сможете войти в
            кабинет по своему email. Если оплата не прошла, попробуйте оформить подписку ещё раз.
          </p>
          <div className="mt-6 flex flex-col gap-3 sm:flex-row">
            <Link
              to={localePath('/account', RU)}
              className="rounded-xl px-6 py-3 text-sm font-black"
              style={{ backgroundColor: ink, color: gold }}
            >
              В кабинет
            </Link>
            <Link
              to={localePath('/subscribe', RU)}
              className="rounded-xl border px-6 py-3 text-sm font-bold"
              style={{ borderColor: '#ddd0b8', color: muted }}
            >
              К оформлению
            </Link>
          </div>
        </>
      ) : (
        <>
          <div
            className="h-10 w-10 animate-spin rounded-full border-4 border-transparent"
            style={{ borderTopColor: goldDeep, borderRightColor: goldDeep }}
            role="status"
            aria-label="Проверяем оплату"
          />
          <h1 className="mt-6 text-2xl" style={serif}>
            {state === 'active' ? 'Подписка активна!' : 'Подтверждаем оплату…'}
          </h1>
          <p className="mt-2 max-w-sm text-sm" style={{ color: muted }}>
            {state === 'active'
              ? 'Открываем личный кабинет.'
              : 'Обычно это занимает несколько секунд.'}
          </p>
        </>
      )}
    </div>
  )
}
