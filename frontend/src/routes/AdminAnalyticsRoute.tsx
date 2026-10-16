import { useEffect, useMemo, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { AdminNav } from '@/components/admin/AdminNav'

/** Покупка за период: разовый заказ или платёж подписки (первый/продление).
 * Платежи подписок живут не в orders, поэтому у них нет заказа — только paymentId. */
type Purchase = {
  kind?: 'order' | 'subscription'
  flow?: string
  paymentId?: string
  orderId: string
  publicRef: string
  createdAt: string
  email: string | null
  amount: number
  source: string
  campaign: string | null
  details: Record<string, string> | null
  paymentStatus: string
  refunded: boolean
}

type DailyRow = {
  day: string
  source: string
  visitors: number
  orders: number
  purchases: number
  revenue: number
  refunds: number
  refundedRevenue: number
}

/** Блок подписок: считается по всем подпискам, а не по выбранному периоду —
 * «сколько сейчас активных» это вопрос про состояние, а не про срез дней. */
type SubscriptionItem = {
  email: string
  status: string
  source: string
  campaign: string | null
  createdAt: string
  accessUntil: string | null
  durationDays: number
}

type SubscriptionsBlock = {
  total: number
  /** С реальным платежом, без тестовых адресов и неоплаченных. */
  paid: number
  active: number
  pastDue: number
  pending: number
  cancellations: number
  avgDurationDays: number
  bySource: Array<{ source: string; count: number }>
  items: SubscriptionItem[]
  error?: string
}

/** Строка разреза по плечам A/B за период. Визиты берутся из visits, поэтому
 * конверсия считается от визита, а не от заказа: плечо с более коротким путём
 * до заказа иначе выигрывало бы автоматически. */
type ArmRow = {
  arm: string
  visits: number
  orders: number
  purchases: number
  revenue: number
}

/** Воронка партнёра по плечам показа: показы и клики — люди, остальное — заказы. */
type PartnerArmRow = {
  arm: string
  shown: number
  added: number
  declined: number
  paid: number
  briefed: number
  ready: number
  canceled: number
  revenue: number
}

const PARTNER_ARM_LABEL: Record<string, string> = {
  cart: 'LesRoses — cart',
  popup: 'LesRoses — popup',
  manual: 'LesRoses — вручную',
}

type Analytics = {
  days: number
  since: string
  arms?: ArmRow[]
  partners?: { lesroses?: PartnerArmRow[] | null }
  purchases: Purchase[]
  daily: DailyRow[]
  subscriptions?: SubscriptionsBlock
}

const SUB_STATUS_LABELS: Record<string, string> = {
  active: 'активна',
  past_due: 'просрочена',
  pending: 'не оплачена',
  canceled: 'отменена',
}

const PERIODS = [7, 14, 30]

const SUB_FLOW_LABELS: Record<string, string> = {
  subscription_first: 'подписка · первый платёж',
  subscription_rebill: 'подписка · продление',
}

function fmtDateTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })
}

function fmtDay(day: string): string {
  const [y, m, d] = day.split('-')
  return y && m && d ? `${d}.${m}` : day
}

/** Подписи плеч — имя из ссылки `?arm=` плюс цена. То же имя стоит в бейджах
 * /admin/orders, на дашборде Grafana и в ссылках для рекламы: отчёт площадки
 * сверяется с нашей аналитикой по совпадающему имени, а не по памяти. */
const ARM_LABEL: Record<string, string> = {
  v1_base: 'variant1-990 · 990 ₽',
  v1_price: 'variant2-750s-990 · разовая 990 / подписка 750, подписка по умолчанию',
  v1_price_onetime: 'variant2-750-990s · разовая 990 / подписка 750, разовая по умолчанию',
  v1_750: 'variant1-750 · 750 ₽',
  v1_500: 'variant1-500 · 500 ₽',
  v1_500_750s: 'variant2-500-750s · подписка 500 / разовая 750, разовая по умолчанию',
  v1_500s_750: 'variant2-500s-750 · подписка 500 / разовая 750, подписка по умолчанию',
  v2_base: 'v2_late · 990 ₽',
  v2_prepay: 'v2_prepay · 990 ₽',
}

function fmtCr(purchases: number, visitors: number): string {
  if (!visitors) return '—'
  return `${((purchases / visitors) * 100).toFixed(1)}%`
}

/** Полная расшифровка меток — в title, чтобы не раздувать таблицу. */
function detailsTitle(details: Record<string, string> | null): string {
  if (!details) return ''
  return Object.entries(details)
    .map(([k, v]) => `${k}: ${v}`)
    .join('\n')
}

export function AdminAnalyticsRoute() {
  const [data, setData] = useState<Analytics | null>(null)
  const [days, setDays] = useState(14)
  const [reloadKey, setReloadKey] = useState(0)
  const [loadedKey, setLoadedKey] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const requestKey = `${days}|${reloadKey}`

  useEffect(() => {
    let cancelled = false
    fetch(`/api/admin/analytics?days=${days}`, { headers: { Accept: 'application/json' } })
      .then(async (res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const json = (await res.json()) as Analytics
        if (cancelled) return
        setData(json)
        setError(null)
        setLoadedKey(`${days}|${reloadKey}`)
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Ошибка загрузки')
      })
    return () => {
      cancelled = true
    }
  }, [days, reloadKey])

  // Грузимся, пока не доехал ответ на текущий запрос — спиннер работает и на
  // первой загрузке, и при смене периода, и на «Обновить», без setState в эффекте.
  const loading = !error && loadedKey !== requestKey

  // Итоги за период по источникам — из тех же дневных строк, без отдельного запроса.
  const bySource = useMemo(() => {
    const acc = new Map<
      string,
      { visitors: number; orders: number; purchases: number; revenue: number; refunds: number; refundedRevenue: number }
    >()
    for (const row of data?.daily ?? []) {
      const cur = acc.get(row.source) ?? {
        visitors: 0,
        orders: 0,
        purchases: 0,
        revenue: 0,
        refunds: 0,
        refundedRevenue: 0,
      }
      cur.visitors += row.visitors
      cur.orders += row.orders
      cur.purchases += row.purchases
      cur.revenue += row.revenue
      cur.refunds += row.refunds
      cur.refundedRevenue += row.refundedRevenue
      acc.set(row.source, cur)
    }
    return [...acc.entries()].sort((a, b) => b[1].visitors - a[1].visitors || b[1].purchases - a[1].purchases)
  }, [data])

  // Возврат не выручка — считаем только paid, чтобы сумма в заголовке совпадала
  // с колонкой «Выручка» в таблицах.
  const totalRevenue = useMemo(
    () => (data?.purchases ?? []).filter((p) => !p.refunded).reduce((sum, p) => sum + p.amount, 0),
    [data],
  )
  const totalPurchasesCount = useMemo(
    () => (data?.purchases ?? []).filter((p) => !p.refunded).length,
    [data],
  )
  // Подписочная часть выручки отдельно: в кабинете ЮKassa она неотличима от
  // разовых покупок, а в списке заказов её нет вовсе.
  const subPayments = useMemo(() => (data?.purchases ?? []).filter((p) => p.kind === 'subscription'), [data])
  const subRevenue = useMemo(() => subPayments.reduce((sum, p) => sum + p.amount, 0), [subPayments])

  return (
    <div className="min-h-screen bg-gray-50">
      <div className="mx-auto max-w-7xl px-4 py-8">
        <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
          <div>
            <h1 className="text-2xl font-bold text-gray-900">Аналитика</h1>
            <p className="text-sm text-gray-500">
              Покупки и трафик по источникам · {totalPurchasesCount} покупок на {totalRevenue.toLocaleString('ru-RU')} ₽
            </p>
          </div>
          <div className="flex items-center gap-2">
            <AdminNav current="/admin/analytics" />
            <div className="flex overflow-hidden rounded-lg border border-gray-300 bg-white">
              {PERIODS.map((p) => (
                <button
                  key={p}
                  type="button"
                  onClick={() => setDays(p)}
                  className={`px-3 py-2 text-sm font-medium ${
                    days === p ? 'bg-gray-900 text-white' : 'text-gray-600 hover:bg-gray-100'
                  }`}
                >
                  {p} дн
                </button>
              ))}
            </div>
            <button
              type="button"
              onClick={() => setReloadKey((k) => k + 1)}
              className="inline-flex items-center gap-1.5 rounded-lg bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-gray-700"
            >
              <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
              Обновить
            </button>
          </div>
        </div>

        {error && (
          <div className="mb-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700">
            Не удалось загрузить аналитику: {error}
          </div>
        )}

        <h2 className="mb-2 text-lg font-semibold text-gray-900">Плечи A/B за период</h2>
        <p className="mb-2 text-xs text-gray-500">
          Проценты считаются от визитов в плечо. Тестовые заказы из /admin/ads исключены, период —{' '}
          {data?.days ?? days} полных суток UTC. Тренды по дням, воронка по шагам и апсейлы — на{' '}
          <a href="/admin/dashboard/" className="font-medium text-gray-700 underline">
            дашборде
          </a>
          .
        </p>
        <div className="mb-8 overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
          <table className="min-w-full divide-y divide-gray-200 text-sm">
            <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
              <tr>
                <th className="px-3 py-3 font-medium">Плечо</th>
                <th className="px-3 py-3 font-medium">Визиты</th>
                <th className="px-3 py-3 font-medium">Заказы</th>
                <th className="px-3 py-3 font-medium">Покупки</th>
                <th className="px-3 py-3 font-medium">Визит → заказ</th>
                <th className="px-3 py-3 font-medium">Визит → покупка</th>
                <th className="px-3 py-3 font-medium">Выручка</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {(data?.arms ?? []).map((a) => (
                <tr key={a.arm} className="hover:bg-gray-50">
                  <td className="px-3 py-3 font-medium text-gray-800">{ARM_LABEL[a.arm] ?? a.arm}</td>
                  <td className="px-3 py-3 text-gray-600">{a.visits || '—'}</td>
                  <td className="px-3 py-3 text-gray-600">{a.orders || '—'}</td>
                  <td className="px-3 py-3 font-medium text-gray-900">{a.purchases || '—'}</td>
                  <td className="px-3 py-3 text-gray-600">{fmtCr(a.orders, a.visits)}</td>
                  <td className="px-3 py-3 font-medium text-gray-900">{fmtCr(a.purchases, a.visits)}</td>
                  <td className="whitespace-nowrap px-3 py-3 text-gray-800">
                    {a.revenue ? `${a.revenue.toLocaleString('ru-RU')} ₽` : '—'}
                  </td>
                </tr>
              ))}
              {!loading && (data?.arms ?? []).length === 0 && (
                <tr>
                  <td colSpan={7} className="px-3 py-10 text-center text-gray-400">
                    Плечи за период не размечены — либо сплит не включён в настройках, либо визитов ещё нет
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        <h2 className="mb-2 text-lg font-semibold text-gray-900">Партнёр Les Roses</h2>
        <p className="mb-2 text-xs text-gray-500">
          Песни, проданные в корзине lesrosesdechantal.ru и оплаченные там же. В источники и плечи выше не входят. Заказы
          и сверка — в{' '}
          <a href="/admin/lesroses" className="font-medium text-gray-700 underline">
            кабинете Les Roses
          </a>
          .
        </p>
        <div className="mb-8 overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
          <table className="min-w-full divide-y divide-gray-200 text-sm">
            <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
              <tr>
                <th className="px-3 py-3 font-medium">Вариант</th>
                <th className="px-3 py-3 font-medium">Показы</th>
                <th className="px-3 py-3 font-medium">Добавили</th>
                <th className="px-3 py-3 font-medium" title="Закрыли попап: «Нет, спасибо», крестик или клик мимо окна">
                  Закрыли
                </th>
                <th className="px-3 py-3 font-medium">Оплатили</th>
                <th className="px-3 py-3 font-medium">Показ → оплата</th>
                <th className="px-3 py-3 font-medium">Анкета</th>
                <th className="px-3 py-3 font-medium">Готово</th>
                <th className="px-3 py-3 font-medium">Сумма</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {(data?.partners?.lesroses ?? []).map((a) => (
                <tr key={a.arm} className="hover:bg-gray-50">
                  <td className="px-3 py-3 font-medium text-gray-800">{PARTNER_ARM_LABEL[a.arm] ?? a.arm}</td>
                  <td className="px-3 py-3 text-gray-600">{a.shown || '—'}</td>
                  <td className="px-3 py-3 text-gray-600">{a.added || '—'}</td>
                  <td className="px-3 py-3 text-gray-600">{a.declined || '—'}</td>
                  <td className="px-3 py-3 font-medium text-gray-900">{a.paid || '—'}</td>
                  <td className="px-3 py-3 text-gray-600">{fmtCr(a.paid, a.shown)}</td>
                  <td className="px-3 py-3 text-gray-600">{a.briefed || '—'}</td>
                  <td className="px-3 py-3 text-gray-600">{a.ready || '—'}</td>
                  <td className="whitespace-nowrap px-3 py-3 text-gray-800">
                    {a.revenue ? `${a.revenue.toLocaleString('ru-RU')} ₽` : '—'}
                  </td>
                </tr>
              ))}
              {!loading && (data?.partners?.lesroses ?? []).length === 0 && (
                <tr>
                  <td colSpan={9} className="px-3 py-10 text-center text-gray-400">
                    За период ни показов, ни заказов
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        <h2 className="mb-2 text-lg font-semibold text-gray-900">Итого по источникам за период</h2>
        <div className="mb-8 overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
          <table className="min-w-full divide-y divide-gray-200 text-sm">
            <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
              <tr>
                <th className="px-3 py-3 font-medium">Источник</th>
                <th className="px-3 py-3 font-medium">Визиты</th>
                <th className="px-3 py-3 font-medium">Заказы</th>
                <th className="px-3 py-3 font-medium">Покупки</th>
                <th className="px-3 py-3 font-medium">Конверсия</th>
                <th className="px-3 py-3 font-medium">Выручка</th>
                <th className="px-3 py-3 font-medium">Возвраты</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {bySource.map(([source, s]) => (
                <tr key={source} className="hover:bg-gray-50">
                  <td className="px-3 py-3 font-medium text-gray-800">{source}</td>
                  <td className="px-3 py-3 text-gray-600">{s.visitors || '—'}</td>
                  <td className="px-3 py-3 text-gray-600">{s.orders || '—'}</td>
                  <td className="px-3 py-3 font-medium text-gray-900">{s.purchases || '—'}</td>
                  <td className="px-3 py-3 text-gray-600">{fmtCr(s.purchases, s.visitors)}</td>
                  <td className="whitespace-nowrap px-3 py-3 text-gray-800">
                    {s.revenue ? `${s.revenue.toLocaleString('ru-RU')} ₽` : '—'}
                  </td>
                  <td className="whitespace-nowrap px-3 py-3 text-red-600">
                    {s.refunds ? `${s.refunds} / ${s.refundedRevenue.toLocaleString('ru-RU')} ₽` : '—'}
                  </td>
                </tr>
              ))}
              {!loading && bySource.length === 0 && (
                <tr>
                  <td colSpan={7} className="px-3 py-10 text-center text-gray-400">
                    Данных за период нет
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        <h2 className="mb-2 text-lg font-semibold text-gray-900">Подписки</h2>
        {data?.subscriptions?.error ? (
          <div className="mb-8 rounded-xl border border-gray-200 bg-white px-4 py-3 text-sm text-red-700 shadow-sm">
            Не удалось посчитать подписки
          </div>
        ) : (
          <div className="mb-8 space-y-4">
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
              {[
                { label: 'Оплаченных подписок', value: data?.subscriptions?.paid ?? 0 },
                { label: 'Всего оформлений', value: data?.subscriptions?.total ?? 0 },
                { label: 'Активных', value: data?.subscriptions?.active ?? 0 },
                { label: 'Отписок', value: data?.subscriptions?.cancellations ?? 0 },
                { label: 'Просрочено', value: data?.subscriptions?.pastDue ?? 0 },
                {
                  label: 'Средняя длительность',
                  value: `${data?.subscriptions?.avgDurationDays ?? 0} дн.`,
                },
                { label: `Платежей за ${data?.days ?? 14} дн.`, value: subPayments.length },
                { label: `Выручка подписок за ${data?.days ?? 14} дн.`, value: `${subRevenue.toLocaleString('ru-RU')} ₽` },
              ].map((card) => (
                <div key={card.label} className="rounded-xl border border-gray-200 bg-white p-4 shadow-sm">
                  <div className="text-xs uppercase tracking-wide text-gray-500">{card.label}</div>
                  <div className="mt-1 text-2xl font-bold text-gray-900">{card.value}</div>
                </div>
              ))}
            </div>

            <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
              <table className="min-w-full divide-y divide-gray-200 text-sm">
                <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
                  <tr>
                    <th className="px-3 py-3 font-medium">Подписчик</th>
                    <th className="px-3 py-3 font-medium">Статус</th>
                    <th className="px-3 py-3 font-medium">Источник</th>
                    <th className="px-3 py-3 font-medium">Оформлена</th>
                    <th className="px-3 py-3 font-medium">Длительность</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {(data?.subscriptions?.items ?? []).map((sub) => (
                    <tr key={sub.email} className="hover:bg-gray-50">
                      <td className="px-3 py-3 text-gray-800">{sub.email}</td>
                      <td className="px-3 py-3 text-gray-600">{SUB_STATUS_LABELS[sub.status] ?? sub.status}</td>
                      <td className="px-3 py-3 text-gray-600" title={sub.campaign ?? ''}>
                        {sub.source}
                      </td>
                      <td className="whitespace-nowrap px-3 py-3 text-gray-600">{fmtDateTime(sub.createdAt)}</td>
                      <td className="whitespace-nowrap px-3 py-3 text-gray-600">
                        {sub.status === 'pending' ? '—' : `${sub.durationDays} дн.`}
                      </td>
                    </tr>
                  ))}
                  {!loading && (data?.subscriptions?.items ?? []).length === 0 && (
                    <tr>
                      <td colSpan={5} className="px-3 py-10 text-center text-gray-400">
                        Подписок пока нет
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        )}

        <h2 className="mb-2 text-lg font-semibold text-gray-900">Покупки</h2>
        <div className="mb-8 overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
          <table className="min-w-full divide-y divide-gray-200 text-sm">
            <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
              <tr>
                <th className="px-3 py-3 font-medium">Когда</th>
                <th className="px-3 py-3 font-medium">Email</th>
                <th className="px-3 py-3 font-medium">Источник</th>
                <th className="px-3 py-3 font-medium">Кампания</th>
                <th className="px-3 py-3 font-medium">Сумма</th>
                <th className="px-3 py-3 font-medium">Заказ</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {(data?.purchases ?? []).map((p) => (
                <tr
                  key={p.orderId || p.paymentId}
                  className={`hover:bg-gray-50 ${p.refunded ? 'bg-red-50/50' : ''}`}
                  title={detailsTitle(p.details)}
                >
                  <td className="whitespace-nowrap px-3 py-3 text-gray-600">{fmtDateTime(p.createdAt)}</td>
                  <td className="px-3 py-3 text-gray-800">{p.email || '—'}</td>
                  <td className="px-3 py-3 font-medium text-gray-900">{p.source}</td>
                  <td className="px-3 py-3 text-gray-600">{p.campaign || '—'}</td>
                  <td className="whitespace-nowrap px-3 py-3 text-gray-800">
                    <span className={p.refunded ? 'text-red-600 line-through' : ''}>{p.amount} ₽</span>
                    {p.refunded && (
                      <span
                        className="ml-2 rounded bg-red-100 px-1.5 py-0.5 text-xs font-medium text-red-700"
                        title="Деньги возвращены клиенту"
                      >
                        возврат
                      </span>
                    )}
                  </td>
                  <td className="px-3 py-3">
                    {p.kind === 'subscription' ? (
                      <span className="rounded bg-violet-100 px-1.5 py-0.5 text-xs font-medium text-violet-700" title={p.paymentId}>
                        {SUB_FLOW_LABELS[p.flow ?? ''] ?? 'подписка'}
                      </span>
                    ) : (
                      <a href={`/order/${p.publicRef}`} className="font-mono text-xs text-rose-600 hover:underline">
                        {p.orderId}
                      </a>
                    )}
                  </td>
                </tr>
              ))}
              {!loading && (data?.purchases.length ?? 0) === 0 && (
                <tr>
                  <td colSpan={6} className="px-3 py-10 text-center text-gray-400">
                    Покупок за период нет
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        <h2 className="mb-2 text-lg font-semibold text-gray-900">По дням</h2>
        <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
          <table className="min-w-full divide-y divide-gray-200 text-sm">
            <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
              <tr>
                <th className="px-3 py-3 font-medium">День</th>
                <th className="px-3 py-3 font-medium">Источник</th>
                <th className="px-3 py-3 font-medium">Визиты</th>
                <th className="px-3 py-3 font-medium">Заказы</th>
                <th className="px-3 py-3 font-medium">Покупки</th>
                <th className="px-3 py-3 font-medium">Конверсия</th>
                <th className="px-3 py-3 font-medium">Выручка</th>
                <th className="px-3 py-3 font-medium">Возвраты</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {(data?.daily ?? []).map((row) => (
                <tr key={`${row.day}|${row.source}`} className="hover:bg-gray-50">
                  <td className="whitespace-nowrap px-3 py-3 text-gray-600">{fmtDay(row.day)}</td>
                  <td className="px-3 py-3 text-gray-800">{row.source}</td>
                  <td className="px-3 py-3 text-gray-600">{row.visitors || '—'}</td>
                  <td className="px-3 py-3 text-gray-600">{row.orders || '—'}</td>
                  <td className="px-3 py-3 font-medium text-gray-900">{row.purchases || '—'}</td>
                  <td className="px-3 py-3 text-gray-600">{fmtCr(row.purchases, row.visitors)}</td>
                  <td className="whitespace-nowrap px-3 py-3 text-gray-800">
                    {row.revenue ? `${row.revenue.toLocaleString('ru-RU')} ₽` : '—'}
                  </td>
                  <td className="whitespace-nowrap px-3 py-3 text-red-600">
                    {row.refunds ? `${row.refunds} / ${row.refundedRevenue.toLocaleString('ru-RU')} ₽` : '—'}
                  </td>
                </tr>
              ))}
              {!loading && (data?.daily.length ?? 0) === 0 && (
                <tr>
                  <td colSpan={8} className="px-3 py-10 text-center text-gray-400">
                    Данных за период нет
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        <p className="mt-4 text-xs text-gray-400">
          Визиты — уникальные сессии на лендинге (счётчик работает с 29.07.2026). «Неизвестно» —
          заказы, созданные до появления меток. Внутренние заказы из /admin/ads в отчёт не входят.
        </p>
      </div>
    </div>
  )
}
