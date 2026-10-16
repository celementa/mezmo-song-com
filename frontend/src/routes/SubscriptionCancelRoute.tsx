import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { apiGet, apiPost } from '@/lib/http'

const ink = '#151210'
const muted = '#6b6558'
const goldDeep = '#8a6621'
const serif = { fontFamily: "'Prata', serif" }

function fmtDate(iso: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' })
}

/**
 * Публичная отмена подписки — требование честной рекуррентки для ЮKassa.
 * Без токена: форма email → письмо со ссылкой. С токеном (?t= из письма):
 * подтверждение кнопкой — отмена только по POST, чтобы префетчер почты,
 * открыв ссылку, не отменил подписку сам.
 */
export function SubscriptionCancelRoute() {
  const [params] = useSearchParams()
  const token = params.get('t') ?? ''

  const [email, setEmail] = useState('')
  const [sent, setSent] = useState(false)
  const [loading, setLoading] = useState(false)
  const [info, setInfo] = useState<{ email: string; canceled: boolean; accessUntil: string | null } | null>(null)
  const [infoError, setInfoError] = useState(false)
  const [done, setDone] = useState<{ accessUntil: string | null } | null>(null)

  useEffect(() => {
    if (!token) return
    apiGet<{ email: string; canceled: boolean; accessUntil: string | null }>(
      `/api/subscription/cancel-info?t=${encodeURIComponent(token)}`,
    )
      .then(setInfo)
      .catch(() => setInfoError(true))
  }, [token])

  const requestLink = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    try {
      await apiPost('/api/subscription/cancel-request', { email: email.trim() })
      setSent(true)
    } finally {
      setLoading(false)
    }
  }

  const confirmCancel = async () => {
    setLoading(true)
    try {
      const res = await apiPost<{ ok: boolean; accessUntil: string | null }>('/api/subscription/cancel', { token })
      setDone({ accessUntil: res.accessUntil })
    } finally {
      setLoading(false)
    }
  }

  const card = (children: React.ReactNode) => (
    <div
      className="flex min-h-screen flex-col items-center px-4 pt-10"
      style={{ backgroundColor: '#f9f4ed', color: ink, fontFamily: "'Onest', sans-serif" }}
    >
      <div className="w-full max-w-md">
        <Link to="/" className="text-sm font-bold hover:opacity-70 transition" style={{ color: muted }}>
          ← На главную
        </Link>
        <div className="mt-4 rounded-3xl bg-white p-6 md:p-8 shadow-md">{children}</div>
      </div>
    </div>
  )

  // --- Ветка с токеном из письма ---
  if (token) {
    if (done) {
      return card(
        <>
          <h1 className="text-2xl mb-3" style={serif}>Подписка отменена</h1>
          <p className="text-sm leading-relaxed" style={{ color: muted }}>
            Автоматические списания прекращены. Доступ к кабинету и песням сохранится до{' '}
            <strong style={{ color: ink }}>{fmtDate(done.accessUntil)}</strong>.
          </p>
          <Link
            to="/account"
            className="mt-6 inline-block rounded-xl px-6 py-3 text-sm font-black"
            style={{ backgroundColor: ink, color: '#F2C572' }}
          >
            В кабинет
          </Link>
        </>,
      )
    }
    if (infoError) {
      return card(
        <>
          <h1 className="text-2xl mb-3" style={serif}>Ссылка недействительна</h1>
          <p className="text-sm leading-relaxed" style={{ color: muted }}>
            Возможно, подписка уже отменена. Запросите новую ссылку по email.
          </p>
          <Link to="/subscription/cancel" className="mt-4 inline-block text-sm underline" style={{ color: goldDeep }}>
            Запросить ссылку
          </Link>
        </>,
      )
    }
    if (!info) {
      return card(<p className="text-sm" style={{ color: muted }}>Загружаем…</p>)
    }
    if (info.canceled) {
      return card(
        <>
          <h1 className="text-2xl mb-3" style={serif}>Подписка уже отменена</h1>
          <p className="text-sm leading-relaxed" style={{ color: muted }}>
            Списаний не будет. Доступ — до <strong style={{ color: ink }}>{fmtDate(info.accessUntil)}</strong>.
          </p>
        </>,
      )
    }
    return card(
      <>
        <h1 className="text-2xl mb-3" style={serif}>Отмена подписки</h1>
        <p className="text-sm leading-relaxed" style={{ color: muted }}>
          Отменить подписку для <strong style={{ color: ink }}>{info.email}</strong>? Списания прекратятся,
          доступ и все песни останутся до <strong style={{ color: ink }}>{fmtDate(info.accessUntil)}</strong>.
        </p>
        <div className="mt-5 flex gap-3">
          <button
            type="button"
            onClick={confirmCancel}
            disabled={loading}
            className="rounded-xl px-5 py-3 text-sm font-bold text-white disabled:opacity-40"
            style={{ backgroundColor: '#8a2121' }}
          >
            {loading ? 'Отменяем…' : 'Отменить подписку'}
          </button>
          <Link
            to="/account"
            className="rounded-xl border px-5 py-3 text-sm font-bold"
            style={{ borderColor: '#ddd0b8', color: muted }}
          >
            Передумал(а)
          </Link>
        </div>
      </>,
    )
  }

  // --- Ветка без токена: запросить ссылку ---
  if (sent) {
    return card(
      <>
        <h1 className="text-2xl mb-3" style={serif}>Проверьте почту</h1>
        <p className="text-sm leading-relaxed" style={{ color: muted }}>
          Если подписка с адресом <strong style={{ color: ink }}>{email}</strong> существует, мы отправили на него
          ссылку для отмены.
        </p>
      </>,
    )
  }
  return card(
    <>
      <h1 className="text-2xl mb-1" style={serif}>Отмена подписки</h1>
      <p className="text-sm mb-6" style={{ color: muted }}>
        Введите email, указанный при оформлении, — мы пришлём ссылку для отмены. Отменить можно и кнопкой в{' '}
        <Link to="/account" className="underline" style={{ color: goldDeep }}>личном кабинете</Link>.
      </p>
      <form onSubmit={requestLink} className="space-y-4">
        <input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
          placeholder="you@example.com"
          className="w-full rounded-xl border px-4 py-3 text-sm focus:outline-none"
          style={{ borderColor: '#ddd0b8' }}
        />
        <button
          type="submit"
          disabled={loading}
          className="w-full rounded-xl px-6 py-3.5 text-sm font-black disabled:opacity-40"
          style={{ backgroundColor: ink, color: '#F2C572' }}
        >
          {loading ? 'Отправляем…' : 'Выслать ссылку'}
        </button>
      </form>
    </>,
  )
}
