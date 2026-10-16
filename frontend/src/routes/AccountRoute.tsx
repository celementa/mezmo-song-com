import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Loader2, LogOut, Music, Plus } from 'lucide-react'
import { apiGet, apiPost } from '@/lib/http'

const ink = '#151210'
const gold = '#F2C572'
const goldDeep = '#8a6621'
const muted = '#6b6558'
const cream = '#f9f4ed'
const serif = { fontFamily: "'Prata', serif" }

type Me = {
  email: string
  name: string | null
  status: string
  accessUntil: string | null
  nextChargeAt: string | null
  canceled: boolean
  priceRub: number
  dailyLimit: number
  usedToday: number
}

type SubOrder = {
  orderId: string
  recipientName: string
  status: string
  deliveryStatus: string
  isFallback: boolean
  createdAt: string
}

function fmtDate(iso: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' })
}

const ORDER_STATUS: Record<string, string> = {
  created: 'в очереди',
  processing: 'генерится…',
  completed: 'готова',
  failed: 'ошибка',
}

// Личный кабинет подписчика: вход по email (без пароля — сессию выдаёт только
// подписка с живым оплаченным периодом), список песен, новая песня, отмена.
export function AccountRoute() {
  const [me, setMe] = useState<Me | null>(null)
  const [checked, setChecked] = useState(false)
  const [orders, setOrders] = useState<SubOrder[]>([])
  const [email, setEmail] = useState('')
  const [loginError, setLoginError] = useState<string | null>(null)
  const [loginLoading, setLoginLoading] = useState(false)
  const [cancelOpen, setCancelOpen] = useState(false)
  const [cancelLoading, setCancelLoading] = useState(false)

  const load = useCallback(async () => {
    try {
      const meRes = await apiGet<Me>('/api/subscription/me')
      setMe(meRes)
      const ordersRes = await apiGet<{ orders: SubOrder[] }>('/api/subscription/orders')
      setOrders(ordersRes.orders)
    } catch {
      setMe(null)
    } finally {
      setChecked(true)
    }
  }, [])

  useEffect(() => {
    // Через микротаск: setState в load всегда асинхронен относительно эффекта
    // (правило react-hooks/set-state-in-effect в этом проекте — error).
    void Promise.resolve().then(load)
  }, [load])

  // Пока песня генерится, страница сама подтягивает свежие статусы.
  useEffect(() => {
    if (!me || !orders.some((o) => o.status === 'created' || o.status === 'processing')) return
    const id = setInterval(() => void load(), 10000)
    return () => clearInterval(id)
  }, [me, orders, load])

  const login = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoginError(null)
    setLoginLoading(true)
    try {
      await apiPost('/api/subscription/session', { email: email.trim() })
      await load()
    } catch (err) {
      const msg = err instanceof Error ? err.message : ''
      setLoginError(
        msg === 'access_expired'
          ? 'Оплаченный период закончился. Оформите подписку заново.'
          : 'Подписка с таким email не найдена.',
      )
    } finally {
      setLoginLoading(false)
    }
  }

  const logout = async () => {
    await apiPost('/api/subscription/logout', {}).catch(() => {})
    setMe(null)
    setOrders([])
  }

  const cancelSubscription = async () => {
    setCancelLoading(true)
    try {
      await apiPost('/api/subscription/cancel', {})
      setCancelOpen(false)
      await load()
    } finally {
      setCancelLoading(false)
    }
  }

  const shell = (children: React.ReactNode) => (
    <div className="min-h-screen" style={{ backgroundColor: cream, color: ink, fontFamily: "'Onest', sans-serif" }}>
      <header className="mx-auto flex max-w-3xl items-center justify-between px-5 py-5">
        <Link to="/" className="text-sm font-bold hover:opacity-70 transition" style={{ color: muted }}>
          ← На главную
        </Link>
        {me && (
          <button
            type="button"
            onClick={logout}
            className="inline-flex items-center gap-1.5 text-sm font-bold hover:opacity-70 transition"
            style={{ color: muted }}
          >
            <LogOut className="h-4 w-4" />
            Выйти
          </button>
        )}
      </header>
      <main className="mx-auto max-w-3xl px-4 pb-16">{children}</main>
    </div>
  )

  if (!checked) {
    return shell(
      <div className="flex justify-center py-24">
        <Loader2 className="h-8 w-8 animate-spin" style={{ color: goldDeep }} />
      </div>,
    )
  }

  if (!me) {
    return shell(
      <div className="mx-auto max-w-md">
        <div className="rounded-3xl bg-white p-6 md:p-8 shadow-md">
          <h1 className="text-2xl mb-1" style={serif}>Личный кабинет</h1>
          <p className="text-sm mb-6" style={{ color: muted }}>
            Введите email, на который оформлена подписка.
          </p>
          <form onSubmit={login} className="space-y-4">
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              placeholder="you@example.com"
              className="w-full rounded-xl border px-4 py-3 text-sm focus:outline-none"
              style={{ borderColor: '#ddd0b8' }}
            />
            {loginError && (
              <div className="rounded-xl px-4 py-3 text-sm" style={{ backgroundColor: '#fdeaea', color: '#8a2121' }}>
                {loginError}{' '}
                <Link to="/subscribe" className="underline font-semibold">Оформить подписку</Link>
              </div>
            )}
            <button
              type="submit"
              disabled={loginLoading}
              className="w-full rounded-xl px-8 py-4 font-black text-sm transition-all hover:opacity-90 disabled:opacity-40"
              style={{ backgroundColor: ink, color: gold }}
            >
              {loginLoading ? 'Проверяем…' : 'Войти'}
            </button>
          </form>
          <p className="mt-4 text-center text-xs" style={{ color: muted }}>
            Ещё нет подписки?{' '}
            <Link to="/subscribe" className="underline" style={{ color: goldDeep }}>
              Оформить
            </Link>
          </p>
        </div>
      </div>,
    )
  }

  const limitReached = me.dailyLimit > 0 && me.usedToday >= me.dailyLimit

  return shell(
    <>
      {/* Карточка подписки */}
      <div className="rounded-3xl p-6 md:p-8 text-white shadow-md" style={{ backgroundColor: '#1d1813' }}>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <h1 className="text-2xl md:text-3xl" style={serif}>
              {me.name ? `Здравствуйте, ${me.name}!` : 'Ваша подписка'}
            </h1>
            <p className="mt-1 text-sm" style={{ color: '#e7ddce' }}>{me.email}</p>
          </div>
          <span
            className="rounded-full border px-3.5 py-1.5 text-xs font-bold"
            style={me.canceled ? { borderColor: '#6b6558', color: '#d9cabb' } : { borderColor: goldDeep, color: gold }}
          >
            {me.canceled ? 'отменена' : me.status === 'past_due' ? 'проблема с оплатой' : 'активна'}
          </span>
        </div>
        {/* Строки друг под другом от одной левой линии: в две колонки длинная
            строка списания переносилась и разъезжалась с соседней. */}
        <div className="mt-5 space-y-1.5 text-sm" style={{ color: '#e7ddce' }}>
          <div>
            {me.canceled ? 'Доступ до' : 'Оплачено до'}:{' '}
            <span className="font-bold text-white">{fmtDate(me.accessUntil)}</span>
          </div>
          {!me.canceled && me.nextChargeAt && (
            <div>
              Следующее списание {me.priceRub.toLocaleString('ru-RU')} ₽:{' '}
              <span className="whitespace-nowrap font-bold text-white">{fmtDate(me.nextChargeAt)}</span>
            </div>
          )}
        </div>
        {me.canceled ? (
          <p className="mt-4 text-sm" style={{ color: '#d9cabb' }}>
            Списаний больше не будет. Когда доступ закончится, подписку можно{' '}
            <Link to="/subscribe" className="underline" style={{ color: gold }}>оформить заново</Link>.
          </p>
        ) : (
          <button
            type="button"
            onClick={() => setCancelOpen(true)}
            className="mt-4 text-xs underline transition hover:opacity-70"
            style={{ color: '#d9cabb' }}
          >
            Отменить подписку
          </button>
        )}
      </div>

      {cancelOpen && (
        <div className="mt-3 rounded-3xl bg-white p-6 shadow-md">
          <p className="text-sm" style={{ color: muted }}>
            Отменить подписку? Автосписания прекратятся, доступ и все песни останутся до{' '}
            <strong style={{ color: ink }}>{fmtDate(me.accessUntil)}</strong>.
          </p>
          <div className="mt-4 flex gap-3">
            <button
              type="button"
              onClick={cancelSubscription}
              disabled={cancelLoading}
              className="rounded-xl px-5 py-2.5 text-sm font-bold text-white disabled:opacity-40"
              style={{ backgroundColor: '#8a2121' }}
            >
              {cancelLoading ? 'Отменяем…' : 'Да, отменить'}
            </button>
            <button
              type="button"
              onClick={() => setCancelOpen(false)}
              className="rounded-xl border px-5 py-2.5 text-sm font-bold"
              style={{ borderColor: '#ddd0b8', color: muted }}
            >
              Оставить подписку
            </button>
          </div>
        </div>
      )}

      {/* Новая песня */}
      <div className="mt-6 flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-xl" style={serif}>Мои песни</h2>
        <div className="flex items-center gap-3">
          {me.dailyLimit > 0 && (
            <span className="text-xs" style={{ color: muted }}>
              сегодня {me.usedToday} из {me.dailyLimit}
            </span>
          )}
          <Link
            to="/account/new"
            aria-disabled={limitReached}
            onClick={(e) => limitReached && e.preventDefault()}
            className="inline-flex items-center gap-1.5 rounded-xl px-5 py-3 text-sm font-black transition-all hover:opacity-90"
            style={limitReached ? { backgroundColor: '#ddd0b8', color: muted, cursor: 'not-allowed' } : { backgroundColor: ink, color: gold }}
          >
            <Plus className="h-4 w-4" />
            Новая песня
          </Link>
        </div>
      </div>
      {limitReached && (
        <p className="mt-2 text-xs" style={{ color: muted }}>
          Суточный лимит исчерпан — новые песни станут доступны в течение суток.
        </p>
      )}

      <div className="mt-4 space-y-3">
        {orders.length === 0 && (
          <div className="rounded-3xl bg-white p-8 text-center shadow-md">
            <Music className="mx-auto h-8 w-8" style={{ color: '#ddd0b8' }} />
            <p className="mt-3 text-sm" style={{ color: muted }}>
              Песен пока нет — создайте первую!
            </p>
          </div>
        )}
        {orders.map((o) => (
          <Link
            key={o.orderId}
            to={`/order/${encodeURIComponent(o.orderId)}`}
            className="flex items-center justify-between gap-4 rounded-3xl bg-white p-5 shadow-md transition hover:shadow-lg"
          >
            <div className="min-w-0">
              <div className="truncate font-bold">{o.recipientName ? `Для: ${o.recipientName}` : 'Песня'}</div>
              <div className="mt-0.5 text-xs" style={{ color: muted }}>{fmtDate(o.createdAt)}</div>
            </div>
            <span
              className="shrink-0 rounded-full px-3 py-1 text-xs font-bold"
              style={
                o.status === 'completed'
                  ? { backgroundColor: '#f6dfa3', color: goldDeep }
                  : { backgroundColor: '#f0ddc9', color: muted }
              }
            >
              {ORDER_STATUS[o.status] ?? o.status}
            </span>
          </Link>
        ))}
      </div>
    </>,
  )
}
