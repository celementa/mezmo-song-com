import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { apiGet, apiPost } from '@/lib/http'
import { localePath } from '@/lib/lang'
import { cream, formatSubDate, gold, goldDeep, ink, muted, serif } from '@/features/subscription/styles'

type CancelInfo = { email: string; canceled: boolean; accessUntil: string | null }

// В палитре подписки нет «тревожного» цвета, а кнопка отмены должна читаться
// как разрушающее действие: единственный литерал палитры, заданный здесь.
const danger = '#8a2121'

/**
 * Публичная отмена подписки — требование честной рекуррентки для ЮKassa.
 * Без токена: форма email → письмо со ссылкой (ответ не раскрывает, есть ли
 * подписка). С токеном (?t= из письма): подтверждение кнопкой — отмена только
 * по POST, чтобы префетчер почты, открыв ссылку, не отменил подписку сам.
 */
export function SubscriptionCancelRoute() {
  const [params] = useSearchParams()
  const token = params.get('t') ?? ''

  const [email, setEmail] = useState('')
  const [sent, setSent] = useState(false)
  const [loading, setLoading] = useState(false)
  const [info, setInfo] = useState<CancelInfo | null>(null)
  const [infoError, setInfoError] = useState(false)
  const [done, setDone] = useState<{ accessUntil: string | null } | null>(null)
  const [actionError, setActionError] = useState('')

  useEffect(() => {
    if (!token) return
    apiGet<CancelInfo>(`/api/subscription/cancel-info?t=${encodeURIComponent(token)}`)
      .then(setInfo)
      .catch(() => setInfoError(true))
  }, [token])

  const requestLink = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    setLoading(true)
    setActionError('')
    try {
      await apiPost<{ ok: boolean }>('/api/subscription/cancel-request', { email: email.trim() })
      setSent(true)
    } catch {
      setActionError('Не удалось отправить письмо. Проверьте адрес и попробуйте ещё раз.')
    } finally {
      setLoading(false)
    }
  }

  const confirmCancel = async () => {
    setLoading(true)
    setActionError('')
    try {
      const res = await apiPost<{ ok: boolean; accessUntil: string | null }>('/api/subscription/cancel', { token })
      setDone({ accessUntil: res.accessUntil })
    } catch {
      setActionError('Не удалось отменить подписку. Обновите страницу и попробуйте ещё раз.')
    } finally {
      setLoading(false)
    }
  }

  const card = (children: ReactNode) => (
    <div className="flex min-h-screen flex-col items-center px-4 pt-10" style={{ backgroundColor: cream, color: ink }}>
      <div className="w-full max-w-md">
        <Link
          to={localePath('/', 'ru')}
          className="text-sm font-bold hover:opacity-70 transition"
          style={{ color: muted }}
        >
          ← На главную
        </Link>
        <div className="mt-4 rounded-3xl bg-white p-6 md:p-8 shadow-md">{children}</div>
      </div>
    </div>
  )

  const errorLine = actionError ? (
    <p className="mt-4 text-sm" style={{ color: danger }}>
      {actionError}
    </p>
  ) : null

  // --- Ветка с токеном из письма ---
  if (token) {
    if (done) {
      return card(
        <>
          <h1 className="text-2xl mb-3" style={serif}>
            Подписка отменена
          </h1>
          <p className="text-sm leading-relaxed" style={{ color: muted }}>
            Автоматические списания прекращены, повторных платежей не будет.
            {done.accessUntil ? (
              <>
                {' '}
                Доступ к кабинету и песням сохранится до{' '}
                <strong style={{ color: ink }}>{formatSubDate(done.accessUntil)}</strong>.
              </>
            ) : null}
          </p>
          <Link
            to={localePath('/account', 'ru')}
            className="mt-6 inline-block rounded-xl px-6 py-3 text-sm font-black"
            style={{ backgroundColor: ink, color: gold }}
          >
            В кабинет
          </Link>
        </>,
      )
    }
    if (infoError) {
      return card(
        <>
          <h1 className="text-2xl mb-3" style={serif}>
            Ссылка недействительна
          </h1>
          <p className="text-sm leading-relaxed" style={{ color: muted }}>
            Возможно, ссылка устарела или подписка уже отменена. Запросите новую ссылку по email.
          </p>
          <Link
            to={localePath('/subscription/cancel', 'ru')}
            className="mt-4 inline-block text-sm underline"
            style={{ color: goldDeep }}
          >
            Запросить новую ссылку
          </Link>
        </>,
      )
    }
    if (!info) {
      return card(
        <p className="text-sm" style={{ color: muted }}>
          Загружаем…
        </p>,
      )
    }
    if (info.canceled) {
      return card(
        <>
          <h1 className="text-2xl mb-3" style={serif}>
            Подписка уже отменена
          </h1>
          <p className="text-sm leading-relaxed" style={{ color: muted }}>
            Списаний не будет.
            {info.accessUntil ? (
              <>
                {' '}
                Доступ — до <strong style={{ color: ink }}>{formatSubDate(info.accessUntil)}</strong>.
              </>
            ) : null}
          </p>
        </>,
      )
    }
    return card(
      <>
        <h1 className="text-2xl mb-3" style={serif}>
          Отмена подписки
        </h1>
        <p className="text-sm leading-relaxed" style={{ color: muted }}>
          Отменить подписку для <strong style={{ color: ink }}>{info.email}</strong>? Списания прекратятся, а
          доступ и все песни останутся
          {info.accessUntil ? (
            <>
              {' '}
              до <strong style={{ color: ink }}>{formatSubDate(info.accessUntil)}</strong>
            </>
          ) : null}
          .
        </p>
        <div className="mt-5 flex flex-wrap gap-3">
          <button
            type="button"
            onClick={confirmCancel}
            disabled={loading}
            className="rounded-xl px-5 py-3 text-sm font-bold text-white disabled:opacity-40"
            style={{ backgroundColor: danger }}
          >
            {loading ? 'Отменяем…' : 'Отменить подписку'}
          </button>
          <Link
            to={localePath('/account', 'ru')}
            className="rounded-xl border px-5 py-3 text-sm font-bold"
            style={{ borderColor: '#ddd0b8', color: muted }}
          >
            Передумал(а)
          </Link>
        </div>
        {errorLine}
      </>,
    )
  }

  // --- Ветка без токена: запросить ссылку ---
  if (sent) {
    return card(
      <>
        <h1 className="text-2xl mb-3" style={serif}>
          Проверьте почту
        </h1>
        <p className="text-sm leading-relaxed" style={{ color: muted }}>
          Если подписка с адресом <strong style={{ color: ink }}>{email}</strong> существует, мы отправили на него
          ссылку для отмены. Письмо приходит в течение пары минут — загляните и в папку «Спам».
        </p>
        <Link
          to={localePath('/account', 'ru')}
          className="mt-6 inline-block rounded-xl px-6 py-3 text-sm font-black"
          style={{ backgroundColor: ink, color: gold }}
        >
          В кабинет
        </Link>
      </>,
    )
  }
  return card(
    <>
      <h1 className="text-2xl mb-1" style={serif}>
        Отмена подписки
      </h1>
      <p className="text-sm mb-6" style={{ color: muted }}>
        Введите email, указанный при оформлении, — мы пришлём ссылку для отмены. Отменить подписку можно и кнопкой в{' '}
        <Link to={localePath('/account', 'ru')} className="underline" style={{ color: goldDeep }}>
          личном кабинете
        </Link>
        .
      </p>
      <form onSubmit={requestLink} className="space-y-4">
        <input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
          aria-label="Email, указанный при оформлении подписки"
          placeholder="you@example.com"
          className="w-full rounded-xl border px-4 py-3 text-sm focus:outline-none"
          style={{ borderColor: '#ddd0b8' }}
        />
        <button
          type="submit"
          disabled={loading}
          className="w-full rounded-xl px-6 py-3.5 text-sm font-black disabled:opacity-40"
          style={{ backgroundColor: ink, color: gold }}
        >
          {loading ? 'Отправляем…' : 'Выслать ссылку'}
        </button>
      </form>
      {errorLine}
    </>,
  )
}
