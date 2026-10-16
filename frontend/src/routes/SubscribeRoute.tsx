import { useCallback, useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { Infinity as InfinityIcon, Music, ShieldCheck, XCircle } from 'lucide-react'
import { getAttribution } from '@/lib/attribution'
import { apiGet, apiPost } from '@/lib/http'
import { YooKassaWidget } from '@/features/payment/YooKassaWidget'
import type { PaymentMode, PaymentStart } from '@/features/payment/useEmbeddedPayment'

const DEFAULT_PRICE = 990

// Тихий запуск: страница живёт по прямой ссылке /subscribe, с главной на неё
// пока не ведём. Дизайн — язык gold-лендинга: крем, чернильные панели, золото,
// Prata для заголовков.
const ink = '#151210'
const inkSoft = '#1d1813'
const gold = '#F2C572'
const goldDeep = '#8a6621'
const muted = '#6b6558'
const cream = '#f9f4ed'
const serif = { fontFamily: "'Prata', serif" }

const PERKS = [
  {
    icon: InfinityIcon,
    title: 'Песни без счётчика',
    text: 'Создавайте песню на каждый повод — дни рождения, годовщины, просто так. До 5 новых песен в сутки.',
  },
  {
    icon: Music,
    title: 'Доработки без ограничений',
    text: 'Меняйте текст, стиль и голос сколько угодно, пока песня не станет той самой.',
  },
  {
    icon: ShieldCheck,
    title: 'Личный кабинет',
    text: 'Все ваши песни в одном месте, вход по email — без паролей.',
  },
  {
    icon: XCircle,
    title: 'Отмена в один клик',
    text: 'Кнопка отмены в кабинете и ссылка в каждом письме. Доступ — до конца оплаченного месяца.',
  },
]

function clientIDs(): { gaClientId?: string; ymClientId?: string } {
  if (typeof document === 'undefined') return {}
  const gaMatch = document.cookie.match(/_ga=GA\d\.\d\.(\d+\.\d+)/)
  const ymMatch = document.cookie.match(/_ym_uid=(\d+)/)
  return {
    ...(gaMatch ? { gaClientId: gaMatch[1] } : {}),
    ...(ymMatch ? { ymClientId: ymMatch[1] } : {}),
  }
}

export function SubscribeRoute() {
  const [params] = useSearchParams()
  const [name, setName] = useState('')
  const [email, setEmail] = useState(params.get('email') ?? '')
  // Ссылка «продлить» из письма несёт sid: у такой подписки своя цена (плечо
  // теста), и согласие на списание должно называть именно её.
  const [price, setPrice] = useState(DEFAULT_PRICE)
  const sid = params.get('sid')
  useEffect(() => {
    if (!sid) return
    apiGet<{ price?: number }>(`/api/subscription/status?sid=${encodeURIComponent(sid)}`)
      .then((res) => {
        if (res.price && res.price > 0) setPrice(res.price)
      })
      .catch(() => {
        /* цена по умолчанию */
      })
  }, [sid])
  const [recurringConsent, setRecurringConsent] = useState(false)
  const [offerConsent, setOfferConsent] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Виджет ЮKassa вместо формы: первый платёж с привязкой карты проходит на
  // своём домене; после оплаты виджет уводит на /subscribe/return.
  const [widget, setWidget] = useState<{ token: string; returnUrl: string } | null>(null)

  const start = useCallback(
    async (mode: PaymentMode) => {
      setError(null)
      setLoading(true)
      try {
        const res = await apiPost<PaymentStart & { sid: string }>(
          '/api/subscription/start',
          // Рекламные метки визита едут вместе с оформлением: без них источник
          // прихода подписки в /admin/analytics остаётся неизвестным.
          {
            email: email.trim(),
            name: name.trim(),
            mode,
            attribution: getAttribution() ?? undefined,
            // Идентификаторы GA4/Метрики — для серверной конверсии по платежу
            // подписки (те же куки, что снимает /create при создании заказа).
            ...clientIDs(),
          },
        )
        if (res.confirmationToken && res.returnUrl && mode === 'embedded') {
          setWidget({ token: res.confirmationToken, returnUrl: res.returnUrl })
          setLoading(false)
          return
        }
        if (res.confirmationUrl) {
          window.location.href = res.confirmationUrl
          return
        }
        // Dev-режим без ЮKassa: подписка активирована сразу.
        window.location.href = `/subscribe/return?sid=${encodeURIComponent(res.sid)}&email=${encodeURIComponent(email.trim())}`
      } catch (err) {
        const msg = err instanceof Error ? err.message : ''
        setError(
          msg === 'already_active'
            ? 'У этого email уже есть активная подписка — войдите в кабинет.'
            : 'Не получилось начать оплату. Попробуйте ещё раз.',
        )
        setLoading(false)
      }
    },
    [email, name],
  )

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    void start('embedded')
  }

  /** Виджет не поднялся — тот же платёж редиректом на страницу ЮKassa. */
  const widgetFail = useCallback(
    (reason: string) => {
      console.warn('[sub] widget failed, fallback to redirect:', reason)
      setWidget(null)
      void start('redirect')
    },
    [start],
  )

  return (
    <div className="min-h-screen" style={{ backgroundColor: cream, color: ink, fontFamily: "'Onest', sans-serif" }}>
      <style>{`
        @keyframes subFadeUp { from { opacity: 0; transform: translateY(14px); } to { opacity: 1; transform: none; } }
        .sub-reveal { animation: subFadeUp 0.6s ease-out both; }
      `}</style>

      <header className="mx-auto flex max-w-5xl items-center justify-between px-5 py-5">
        <Link to="/" className="text-sm font-bold hover:opacity-70 transition" style={{ color: muted }}>
          ← На главную
        </Link>
        <Link to="/account" className="text-sm font-bold hover:opacity-70 transition" style={{ color: goldDeep }}>
          Уже с подпиской? В кабинет
        </Link>
      </header>

      {/* Тёмная hero-панель в духе gold-лендинга */}
      <section className="mx-4 md:mx-auto max-w-5xl rounded-3xl overflow-hidden sub-reveal" style={{ backgroundColor: inkSoft }}>
        <div className="px-6 py-10 md:px-12 md:py-14 text-white">
          <div
            className="inline-block px-3.5 py-1.5 rounded-full text-xs font-bold mb-5 border"
            style={{ borderColor: goldDeep, color: gold }}
          >
            Подписка
          </div>
          <h1 className="text-3xl md:text-5xl leading-tight mb-4" style={serif}>
            Песни без границ
          </h1>
          <p className="max-w-xl text-base md:text-lg" style={{ color: '#e7ddce' }}>
            Безлимит персональных песен и доработок за{' '}
            <span className="font-bold" style={{ color: gold }}>{price.toLocaleString('ru-RU')} ₽ в месяц</span>.
            Отменить можно в любой момент.
          </p>
        </div>
      </section>

      <section className="mx-auto max-w-5xl px-4 md:px-0 mt-8 grid gap-3 sm:grid-cols-2 sub-reveal" style={{ animationDelay: '0.12s' }}>
        {PERKS.map((p) => (
          <div key={p.title} className="rounded-3xl bg-white p-6 shadow-md">
            <p.icon className="h-6 w-6 mb-3" style={{ color: goldDeep }} />
            <div className="text-lg font-bold mb-1.5" style={serif}>{p.title}</div>
            <p className="text-sm leading-relaxed" style={{ color: muted }}>{p.text}</p>
          </div>
        ))}
      </section>

      {/* Оформление */}
      <section className="mx-auto max-w-xl px-4 md:px-0 mt-10 pb-16 sub-reveal" style={{ animationDelay: '0.24s' }}>
        <div className="rounded-3xl bg-white p-6 md:p-8 shadow-md">
          <h2 className="text-2xl mb-1" style={serif}>Оформление</h2>
          <p className="text-sm mb-6" style={{ color: muted }}>
            {price.toLocaleString('ru-RU')} ₽ сейчас — и месяц безлимита. Дальше — автоматическое продление раз в
            месяц, с напоминанием на почту за 3 дня до списания.
          </p>

          {widget ? (
            <div className="space-y-4">
              <YooKassaWidget confirmationToken={widget.token} returnUrl={widget.returnUrl} onFail={widgetFail} />
              <p className="text-center text-xs" style={{ color: muted }}>
                Способ оплаты привязывается для продления подписки. Данные карты обрабатывает ЮKassa, мы их не видим.
              </p>
            </div>
          ) : (
          <form onSubmit={submit} className="space-y-4">
            <label className="block">
              <span className="text-sm font-semibold">Ваше имя</span>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                placeholder="Как к вам обращаться"
                className="mt-1.5 w-full rounded-xl border px-4 py-3 text-sm focus:outline-none"
                style={{ borderColor: '#ddd0b8' }}
              />
            </label>
            <label className="block">
              <span className="text-sm font-semibold">Email</span>
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                placeholder="you@example.com"
                className="mt-1.5 w-full rounded-xl border px-4 py-3 text-sm focus:outline-none"
                style={{ borderColor: '#ddd0b8' }}
              />
              <span className="mt-1 block text-xs" style={{ color: muted }}>
                На него придут песни и вход в кабинет.
              </span>
            </label>

            <div className="space-y-3 pt-1 text-sm" style={{ color: muted }}>
              <label className="flex items-start gap-2.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={recurringConsent}
                  onChange={(e) => setRecurringConsent(e.target.checked)}
                  required
                  className="mt-0.5 h-4 w-4"
                />
                <span>
                  Я соглашаюсь на автоматическое списание {price.toLocaleString('ru-RU')} ₽ раз в месяц с привязанной
                  карты. Отменить подписку можно в любой момент на{' '}
                  <Link to="/subscription/cancel" className="underline" style={{ color: goldDeep }}>
                    странице отмены
                  </Link>{' '}
                  или по ссылке из письма.
                </span>
              </label>
              <label className="flex items-start gap-2.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={offerConsent}
                  onChange={(e) => setOfferConsent(e.target.checked)}
                  required
                  className="mt-0.5 h-4 w-4"
                />
                <span>
                  Я принимаю{' '}
                  <Link to="/subscription-offer" className="underline" style={{ color: goldDeep }}>
                    условия подписки
                  </Link>{' '}
                  и{' '}
                  <Link to="/privacy" className="underline" style={{ color: goldDeep }}>
                    политику конфиденциальности
                  </Link>
                  .
                </span>
              </label>
            </div>

            {error && (
              <div className="rounded-xl px-4 py-3 text-sm" style={{ backgroundColor: '#fdeaea', color: '#8a2121' }}>
                {error}{' '}
                {error.includes('кабинет') && (
                  <Link to="/account" className="underline font-semibold">Войти</Link>
                )}
              </div>
            )}

            <button
              type="submit"
              disabled={loading || !recurringConsent || !offerConsent}
              className="w-full rounded-xl px-8 py-4 font-black text-sm transition-all hover:opacity-90 disabled:opacity-40"
              style={{ backgroundColor: ink, color: gold }}
            >
              {loading ? 'Переходим к оплате…' : `Оформить за ${price.toLocaleString('ru-RU')} ₽/мес`}
            </button>
            <p className="text-center text-xs" style={{ color: muted }}>
              Оплата через ЮKassa: карта, СБП, SberPay, T-Pay. Способ оплаты привязывается для продления подписки.
            </p>
          </form>
          )}
        </div>
      </section>
    </div>
  )
}
