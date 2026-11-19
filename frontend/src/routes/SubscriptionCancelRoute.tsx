import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { apiGet, apiPost } from '@/lib/http'
import { localePath, useLang, useTr } from '@/lib/lang'
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
 *
 * Одна страница на оба языка: /ru и /en. Письмо приходит на языке подписки,
 * и ссылка ведёт на ту же языковую версию.
 */
export function SubscriptionCancelRoute() {
  const tr = useTr()
  const lang = useLang()
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
      setActionError(tr(
        'Не удалось отправить письмо. Проверьте адрес и попробуйте ещё раз.',
        "Couldn't send the email. Check the address and try again.",
      ))
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
      setActionError(tr(
        'Не удалось отменить подписку. Обновите страницу и попробуйте ещё раз.',
        "Couldn't cancel the subscription. Refresh the page and try again.",
      ))
    } finally {
      setLoading(false)
    }
  }

  const card = (children: ReactNode) => (
    <div className="flex min-h-screen flex-col items-center px-4 pt-10" style={{ backgroundColor: cream, color: ink }}>
      <div className="w-full max-w-md">
        <Link
          to={localePath('/', lang)}
          className="text-sm font-bold hover:opacity-70 transition"
          style={{ color: muted }}
        >
          {tr('← На главную', '← Back to home')}
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
            {tr('Подписка отменена', 'Subscription canceled')}
          </h1>
          <p className="text-sm leading-relaxed" style={{ color: muted }}>
            {tr(
              'Автоматические списания прекращены, повторных платежей не будет.',
              'Automatic charges have stopped; there will be no further payments.',
            )}
            {done.accessUntil ? (
              <>
                {' '}
                {tr('Доступ к кабинету и песням сохранится до', 'Access to your account and songs remains until')}{' '}
                <strong style={{ color: ink }}>{formatSubDate(done.accessUntil, lang)}</strong>.
              </>
            ) : null}
          </p>
          <Link
            to={localePath('/account', lang)}
            className="mt-6 inline-block rounded-xl px-6 py-3 text-sm font-black"
            style={{ backgroundColor: ink, color: gold }}
          >
            {tr('В кабинет', 'Go to account')}
          </Link>
        </>,
      )
    }
    if (infoError) {
      return card(
        <>
          <h1 className="text-2xl mb-3" style={serif}>
            {tr('Ссылка недействительна', 'This link is invalid')}
          </h1>
          <p className="text-sm leading-relaxed" style={{ color: muted }}>
            {tr(
              'Возможно, ссылка устарела или подписка уже отменена. Запросите новую ссылку по email.',
              'The link may have expired or the subscription is already canceled. Request a new link by email.',
            )}
          </p>
          <Link
            to={localePath('/subscription/cancel', lang)}
            className="mt-4 inline-block text-sm underline"
            style={{ color: goldDeep }}
          >
            {tr('Запросить новую ссылку', 'Request a new link')}
          </Link>
        </>,
      )
    }
    if (!info) {
      return card(
        <p className="text-sm" style={{ color: muted }}>
          {tr('Загружаем…', 'Loading…')}
        </p>,
      )
    }
    if (info.canceled) {
      return card(
        <>
          <h1 className="text-2xl mb-3" style={serif}>
            {tr('Подписка уже отменена', 'Subscription already canceled')}
          </h1>
          <p className="text-sm leading-relaxed" style={{ color: muted }}>
            {tr('Списаний не будет.', 'There will be no charges.')}
            {info.accessUntil ? (
              <>
                {' '}
                {tr('Доступ — до', 'Access until')}{' '}
                <strong style={{ color: ink }}>{formatSubDate(info.accessUntil, lang)}</strong>.
              </>
            ) : null}
          </p>
        </>,
      )
    }
    return card(
      <>
        <h1 className="text-2xl mb-3" style={serif}>
          {tr('Отмена подписки', 'Cancel subscription')}
        </h1>
        <p className="text-sm leading-relaxed" style={{ color: muted }}>
          {info.accessUntil
            ? tr(
                `Отменить подписку для ${info.email}? Списания прекратятся, а доступ и все песни останутся до ${formatSubDate(info.accessUntil, lang)}.`,
                `Cancel the subscription for ${info.email}? Charges will stop, and access and all your songs remain until ${formatSubDate(info.accessUntil, lang)}.`,
              )
            : tr(
                `Отменить подписку для ${info.email}? Списания прекратятся.`,
                `Cancel the subscription for ${info.email}? Charges will stop.`,
              )}
        </p>
        <div className="mt-5 flex flex-wrap gap-3">
          <button
            type="button"
            onClick={confirmCancel}
            disabled={loading}
            className="rounded-xl px-5 py-3 text-sm font-bold text-white disabled:opacity-40"
            style={{ backgroundColor: danger }}
          >
            {loading ? tr('Отменяем…', 'Canceling…') : tr('Отменить подписку', 'Cancel subscription')}
          </button>
          <Link
            to={localePath('/account', lang)}
            className="rounded-xl border px-5 py-3 text-sm font-bold"
            style={{ borderColor: '#ddd0b8', color: muted }}
          >
            {tr('Передумал(а)', 'Never mind')}
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
          {tr('Проверьте почту', 'Check your email')}
        </h1>
        <p className="text-sm leading-relaxed" style={{ color: muted }}>
          {tr(
            `Если подписка с адресом ${email} существует, мы отправили на него ссылку для отмены. Письмо приходит в течение пары минут — загляните и в папку «Спам».`,
            `If a subscription with ${email} exists, we've sent it a cancellation link. It usually arrives within a couple of minutes — check your spam folder too.`,
          )}
        </p>
        <Link
          to={localePath('/account', lang)}
          className="mt-6 inline-block rounded-xl px-6 py-3 text-sm font-black"
          style={{ backgroundColor: ink, color: gold }}
        >
          {tr('В кабинет', 'Go to account')}
        </Link>
      </>,
    )
  }
  return card(
    <>
      <h1 className="text-2xl mb-1" style={serif}>
        {tr('Отмена подписки', 'Cancel subscription')}
      </h1>
      <p className="text-sm mb-6" style={{ color: muted }}>
        {tr('Введите email, указанный при оформлении, — мы пришлём ссылку для отмены. Отменить подписку можно и кнопкой в ', 'Enter the email you used to subscribe and we\'ll send you a cancellation link. You can also cancel from your ')}
        <Link to={localePath('/account', lang)} className="underline" style={{ color: goldDeep }}>
          {tr('личном кабинете', 'account')}
        </Link>
        .
      </p>
      <form onSubmit={requestLink} className="space-y-4">
        <input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
          aria-label={tr('Email, указанный при оформлении подписки', 'Email used to subscribe')}
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
          {loading ? tr('Отправляем…', 'Sending…') : tr('Выслать ссылку', 'Send link')}
        </button>
      </form>
      {errorLine}
    </>,
  )
}
