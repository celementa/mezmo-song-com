import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Loader2, LogOut, Music, Plus } from 'lucide-react'

import { formatMoney, type OrderCurrency } from '@/lib/currency'
import { apiGet, apiPost } from '@/lib/http'
import { localePath, useLang, useTr } from '@/lib/lang'
import {
  cream,
  formatSubDate,
  gold,
  goldDeep,
  ink,
  inkSoft,
  muted,
  serif,
} from '@/features/subscription/styles'

const bodyFont = "'Onest', sans-serif"

// Статус песни меняет бэкенд (генерация идёт минутами), поэтому песни в работе
// страница перезапрашивает сама — без перезагрузки вкладки человеком.
const ORDER_POLL_MS = 10_000

type Me = {
  email: string
  name: string | null
  status: string
  accessUntil: string | null
  nextChargeAt: string | null
  canceled: boolean
  priceRub: number
  price?: number
  currency?: OrderCurrency
  language?: 'ru' | 'en'
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

// Сессии нет — значит, email не нашёлся; оплата была, но период кончился —
// бэкенд отвечает access_expired, и человеку нужен другой следующий шаг.
type LoginError = 'not_found' | 'expired'

const ORDER_STATUS: Record<string, [string, string]> = {
  created: ['в очереди', 'queued'],
  processing: ['генерится…', 'generating…'],
  completed: ['готова', 'ready'],
  failed: ['ошибка', 'failed'],
}

const inWork = (order: SubOrder) => order.status === 'created' || order.status === 'processing'

/** Общая рамка кабинета: возврат на главную виден всегда, выход — только в кабинете. */
function CabinetPage({
  me,
  onLogout,
  children,
}: {
  me: Me | null
  onLogout: () => void
  children: ReactNode
}) {
  const tr = useTr()
  const lang = useLang()
  return (
    <div className="min-h-screen" style={{ backgroundColor: cream, color: ink, fontFamily: bodyFont }}>
      <header className="mx-auto flex max-w-3xl items-center justify-between px-5 py-5">
        <Link
          to={localePath('/', lang)}
          className="text-sm font-bold transition hover:opacity-70"
          style={{ color: muted }}
        >
          {tr('← На главную', '← Back to home')}
        </Link>
        {me && (
          <button
            type="button"
            onClick={onLogout}
            className="inline-flex items-center gap-1.5 text-sm font-bold transition hover:opacity-70"
            style={{ color: muted }}
          >
            <LogOut className="h-4 w-4" />
            {tr('Выйти', 'Sign out')}
          </button>
        )}
      </header>
      <main className="mx-auto max-w-3xl px-4 pb-16">{children}</main>
    </div>
  )
}

// Личный кабинет подписчика: вход по email (без пароля — сессию выдаёт только
// подписка с живым оплаченным периодом), карточка подписки, отмена, список
// песен и переход к созданию новой. Одна страница на оба языка: тексты через
// tr(), ссылки и даты — по языку текущего адреса.
export function AccountRoute() {
  const tr = useTr()
  const lang = useLang()
  const [me, setMe] = useState<Me | null>(null)
  const [checked, setChecked] = useState(false)
  const [orders, setOrders] = useState<SubOrder[]>([])
  const [email, setEmail] = useState('')
  const [loginError, setLoginError] = useState<LoginError | null>(null)
  const [loginLoading, setLoginLoading] = useState(false)
  const [cancelOpen, setCancelOpen] = useState(false)
  const [cancelLoading, setCancelLoading] = useState(false)

  // Сессия живёт в cookie, поэтому отдельного токена нет: и /me, и /orders
  // отвечают по ней. Первый же отказ означает «не вошёл» — показываем форму.
  const load = useCallback(async () => {
    try {
      const meRes = await apiGet<Me>('/api/subscription/me')
      setMe(meRes)
      const ordersRes = await apiGet<{ orders: SubOrder[] }>('/api/subscription/orders')
      setOrders(ordersRes.orders)
    } catch {
      setMe(null)
      setOrders([])
    } finally {
      setChecked(true)
    }
  }, [])

  useEffect(() => {
    // Через микротаск: setState в load всегда асинхронен относительно эффекта
    // (правило react-hooks/set-state-in-effect в этом проекте — error).
    void Promise.resolve().then(load)
  }, [load])

  // Пока хоть одна песня в очереди или генерится, статусы подтягиваются сами.
  useEffect(() => {
    if (!me || !orders.some(inWork)) return
    const id = setInterval(() => void load(), ORDER_POLL_MS)
    return () => clearInterval(id)
  }, [me, orders, load])

  const login = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    setLoginError(null)
    setLoginLoading(true)
    try {
      await apiPost('/api/subscription/session', { email: email.trim() })
      await load()
    } catch (err) {
      const code = err instanceof Error ? err.message : ''
      setLoginError(code === 'access_expired' ? 'expired' : 'not_found')
    } finally {
      setLoginLoading(false)
    }
  }

  // Выход не показывает ошибку, даже если бэкенд не ответил: cookie с сессией
  // всё равно уже не нужна, человек ждёт форму входа.
  const logout = async () => {
    await apiPost('/api/subscription/logout', {}).catch(() => {})
    setMe(null)
    setOrders([])
    setCancelOpen(false)
    setEmail('')
    setLoginError(null)
  }

  const cancelSubscription = async () => {
    setCancelLoading(true)
    try {
      await apiPost('/api/subscription/cancel', {})
      setCancelOpen(false)
    } catch {
      // Повторная отмена (и сетевой сбой) — не ошибка для человека: ниже просто
      // перечитываем подписку, и карточка покажет её настоящее состояние.
    } finally {
      setCancelLoading(false)
    }
    await load()
  }

  if (!checked) {
    return (
      <CabinetPage me={null} onLogout={logout}>
        <div className="flex justify-center py-24">
          <Loader2 className="h-8 w-8 animate-spin" style={{ color: goldDeep }} />
        </div>
      </CabinetPage>
    )
  }

  // EN-версия подписки продаётся в воронке на плече с экраном «подписка /
  // разовая»: отдельной /en/subscribe нет.
  const subscribePath = localePath(lang === 'en' ? '/config?arm=variant3-500-750s' : '/subscribe', lang)

  if (!me) {
    return (
      <CabinetPage me={null} onLogout={logout}>
        <div className="mx-auto max-w-md">
          <div className="rounded-3xl bg-white p-6 shadow-md md:p-8">
            <h1 className="mb-1 text-2xl" style={serif}>
              {tr('Личный кабинет', 'Your account')}
            </h1>
            <p className="mb-6 text-sm" style={{ color: muted }}>
              {tr(
                'Введите email, на который оформлена подписка.',
                'Enter the email you used to subscribe.',
              )}
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
                  {loginError === 'expired' ? (
                    <>
                      {tr('Оплаченный период закончился. Оформите подписку заново:', 'Your paid period has ended. Subscribe again:')}{' '}
                      <Link to={subscribePath} className="font-semibold underline">
                        {tr('оформить подписку', 'subscribe')}
                      </Link>
                      .
                    </>
                  ) : (
                    tr('Подписка с таким email не найдена.', 'No subscription found for this email.')
                  )}
                </div>
              )}
              <button
                type="submit"
                disabled={loginLoading}
                className="w-full rounded-xl px-8 py-4 text-sm font-black transition-all hover:opacity-90 disabled:opacity-40"
                style={{ backgroundColor: ink, color: gold }}
              >
                {loginLoading ? tr('Проверяем…', 'Checking…') : tr('Войти', 'Sign in')}
              </button>
            </form>
            <p className="mt-4 text-center text-xs" style={{ color: muted }}>
              {tr('Ещё нет подписки?', 'No subscription yet?')}{' '}
              <Link to={subscribePath} className="underline" style={{ color: goldDeep }}>
                {tr('Оформить', 'Subscribe')}
              </Link>
            </p>
          </div>
        </div>
      </CabinetPage>
    )
  }

  const limitReached = me.dailyLimit > 0 && me.usedToday >= me.dailyLimit
  const statusLabel = me.canceled
    ? tr('отменена', 'canceled')
    : me.status === 'past_due'
      ? tr('проблема с оплатой', 'payment issue')
      : tr('активна', 'active')
  const price = me.price ?? me.priceRub
  const currency = me.currency ?? 'RUB'
  const orderStatusLabel = (status: string) => {
    const pair = ORDER_STATUS[status] ?? [status, status]
    return tr(pair[0], pair[1])
  }

  return (
    <CabinetPage me={me} onLogout={logout}>
      {/* Карточка подписки */}
      <div className="rounded-3xl p-6 text-white shadow-md md:p-8" style={{ backgroundColor: inkSoft }}>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <h1 className="text-2xl md:text-3xl" style={serif}>
              {me.name
                ? tr(`Здравствуйте, ${me.name}!`, `Hello, ${me.name}!`)
                : tr('Ваша подписка', 'Your subscription')}
            </h1>
            <p className="mt-1 text-sm" style={{ color: '#e7ddce' }}>
              {me.email}
            </p>
          </div>
          <span
            className="rounded-full border px-3.5 py-1.5 text-xs font-bold"
            style={
              me.canceled
                ? { borderColor: muted, color: '#d9cabb' }
                : { borderColor: goldDeep, color: gold }
            }
          >
            {statusLabel}
          </span>
        </div>
        {/* Строки друг под другом от одной левой линии: в две колонки длинная
            строка списания переносилась и разъезжалась с соседней. */}
        <div className="mt-5 space-y-1.5 text-sm" style={{ color: '#e7ddce' }}>
          <div>
            {me.canceled ? tr('Доступ до', 'Access until') : tr('Оплачено до', 'Paid until')}:{' '}
            <span className="font-bold text-white">{formatSubDate(me.accessUntil, lang)}</span>
          </div>
          {!me.canceled && me.nextChargeAt && (
            <div>
              {tr('Следующее списание', 'Next charge')} {formatMoney(price, currency)}:{' '}
              <span className="whitespace-nowrap font-bold text-white">
                {formatSubDate(me.nextChargeAt, lang)}
              </span>
            </div>
          )}
        </div>
        {me.canceled ? (
          <p className="mt-4 text-sm" style={{ color: '#d9cabb' }}>
            {tr('Списаний больше не будет. Когда доступ закончится, подписку можно', 'No more charges. When access ends, you can')}{' '}
            <Link to={subscribePath} className="underline" style={{ color: gold }}>
              {tr('оформить заново', 'subscribe again')}
            </Link>
            .
          </p>
        ) : (
          <button
            type="button"
            onClick={() => setCancelOpen(true)}
            className="mt-4 text-xs underline transition hover:opacity-70"
            style={{ color: '#d9cabb' }}
          >
            {tr('Отменить подписку', 'Cancel subscription')}
          </button>
        )}
      </div>

      {cancelOpen && (
        <div className="mt-3 rounded-3xl bg-white p-6 shadow-md">
          <p className="text-sm" style={{ color: muted }}>
            {tr(
              'Отменить подписку? Автосписания прекратятся. Доступ и все песни останутся до',
              'Cancel your subscription? Automatic charges will stop. Access and all your songs remain until',
            )}{' '}
            <strong style={{ color: ink }}>{formatSubDate(me.accessUntil, lang)}</strong>.
          </p>
          <div className="mt-4 flex gap-3">
            <button
              type="button"
              onClick={cancelSubscription}
              disabled={cancelLoading}
              className="rounded-xl px-5 py-2.5 text-sm font-bold text-white disabled:opacity-40"
              style={{ backgroundColor: '#8a2121' }}
            >
              {cancelLoading ? tr('Отменяем…', 'Canceling…') : tr('Да, отменить', 'Yes, cancel')}
            </button>
            <button
              type="button"
              onClick={() => setCancelOpen(false)}
              className="rounded-xl border px-5 py-2.5 text-sm font-bold"
              style={{ borderColor: '#ddd0b8', color: muted }}
            >
              {tr('Оставить подписку', 'Keep subscription')}
            </button>
          </div>
        </div>
      )}

      {/* Новая песня */}
      <div className="mt-6 flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-xl" style={serif}>
          {tr('Мои песни', 'My songs')}
        </h2>
        <div className="flex items-center gap-3">
          {me.dailyLimit > 0 && (
            <span className="text-xs" style={{ color: muted }}>
              {tr('сегодня', 'today')} {me.usedToday} {tr('из', 'of')} {me.dailyLimit}
            </span>
          )}
          <Link
            to={localePath('/account/new', lang)}
            aria-disabled={limitReached}
            onClick={(e) => limitReached && e.preventDefault()}
            className="inline-flex items-center gap-1.5 rounded-xl px-5 py-3 text-sm font-black transition-all hover:opacity-90"
            style={
              limitReached
                ? { backgroundColor: '#ddd0b8', color: muted, cursor: 'not-allowed' }
                : { backgroundColor: ink, color: gold }
            }
          >
            <Plus className="h-4 w-4" />
            {tr('Новая песня', 'New song')}
          </Link>
        </div>
      </div>
      {limitReached && (
        <p className="mt-2 text-xs" style={{ color: muted }}>
          {tr(
            'Суточный лимит исчерпан — новые песни станут доступны в течение суток.',
            'Daily limit reached — new songs will be available within 24 hours.',
          )}
        </p>
      )}

      <div className="mt-4 space-y-3">
        {orders.length === 0 && (
          <div className="rounded-3xl bg-white p-8 text-center shadow-md">
            <Music className="mx-auto h-8 w-8" style={{ color: '#ddd0b8' }} />
            <p className="mt-3 text-sm" style={{ color: muted }}>
              {tr('Песен пока нет — создайте первую!', 'No songs yet — create your first one!')}
            </p>
          </div>
        )}
        {orders.map((o) => (
          <Link
            key={o.orderId}
            to={localePath(`/order/${encodeURIComponent(o.orderId)}`, lang)}
            className="flex items-center justify-between gap-4 rounded-3xl bg-white p-5 shadow-md transition hover:shadow-lg"
          >
            <div className="min-w-0">
              <div className="truncate font-bold">
                {o.recipientName
                  ? tr(`Для: ${o.recipientName}`, `For: ${o.recipientName}`)
                  : tr('Песня', 'Song')}
              </div>
              <div className="mt-0.5 text-xs" style={{ color: muted }}>
                {formatSubDate(o.createdAt, lang)}
              </div>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              {/* Демо-песня собрана без генерации: человек не должен принять её за свою. */}
              {o.isFallback && (
                <span
                  className="rounded-full px-2 py-0.5 text-[10px] font-bold uppercase tracking-wide"
                  style={{ backgroundColor: '#f0ddc9', color: muted }}
                >
                  {tr('демо', 'demo')}
                </span>
              )}
              <span
                className="rounded-full px-3 py-1 text-xs font-bold"
                style={
                  o.status === 'completed'
                    ? { backgroundColor: '#f6dfa3', color: goldDeep }
                    : { backgroundColor: '#f0ddc9', color: muted }
                }
              >
                {orderStatusLabel(o.status)}
              </span>
            </div>
          </Link>
        ))}
      </div>
    </CabinetPage>
  )
}
