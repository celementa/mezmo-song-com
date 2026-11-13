import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { Infinity as InfinityIcon, Music, ShieldCheck, XCircle } from 'lucide-react'

import { getAttribution } from '@/lib/attribution'
import { apiGet, apiPost } from '@/lib/http'
import { localePath } from '@/lib/lang'
import { YookassaCheckout } from '@/features/payment/YookassaCheckout'
import type { PaymentMode, PaymentStart } from '@/features/payment/UnitPayCheckout'
import { cream, gold, goldDeep, ink, inkSoft, muted, serif } from '@/features/subscription/styles'

const DEFAULT_PRICE = 990

// Страница только русская (в /en маршрута нет), поэтому строки — русскими
// литералами, без tr(), а ссылки — с явным 'ru': человек остаётся в русской
// версии сайта, даже если кука выбора языка говорила обратное.
const RU = 'ru'

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

/** Сообщение об ошибке по коду, который вернул /api/subscription/start. */
function errorMessage(code: string): string {
  if (code === 'already_active') return 'У этого email уже есть активная подписка.'
  if (code === 'subscriptions_unavailable') return 'Подписка временно недоступна. Попробуйте позже.'
  // bad_json, payment_gateway, db и всё незнакомое — одна общая формулировка:
  // человеку важен следующий шаг («попробуйте ещё раз»), а не код ручки.
  return 'Не получилось начать оплату. Попробуйте ещё раз.'
}

export function SubscribeRoute() {
  const [params] = useSearchParams()
  const [name, setName] = useState('')
  const [email, setEmail] = useState(params.get('email') ?? '')
  // sid приходит из письма «продлить»: у такой подписки своя цена (плечо
  // теста), и согласие на списание должно называть именно её.
  const [price, setPrice] = useState(DEFAULT_PRICE)
  // Из апсейла после заказа приходит его номер: цену подписки берём из
  // ценового плеча заказа. Валюта нужна, чтобы не выдать $15 за 15 ₽.
  const [orderCurrency, setOrderCurrency] = useState<string | null>(null)
  const [recurringConsent, setRecurringConsent] = useState(false)
  const [offerConsent, setOfferConsent] = useState(false)
  const [loading, setLoading] = useState(false)
  // Код ошибки, а не текст: по коду решается, показать ли ссылку в кабинет.
  const [errorCode, setErrorCode] = useState<string | null>(null)
  // Виджет ЮKassa вместо формы: первый платёж с привязкой карты проходит на
  // своём домене; после оплаты виджет уводит на /subscribe/return.
  const [widget, setWidget] = useState<{ token: string; returnUrl: string } | null>(null)

  const sid = params.get('sid')
  const orderRef = params.get('order')
  const foreignOrder = orderCurrency !== null && orderCurrency !== 'RUB'

  // Цена: sid важнее заказа — прямые заходы и письма идут без order.
  useEffect(() => {
    if (sid) {
      apiGet<{ price?: number }>(`/api/subscription/status?sid=${encodeURIComponent(sid)}`)
        .then((res) => {
          if (res.price && res.price > 0) setPrice(res.price)
        })
        .catch(() => {
          /* цена по умолчанию */
        })
      return
    }
    if (!orderRef) return
    apiGet<{ subscriptionAmount?: number; currency?: string }>(
      `/api/public/order/${encodeURIComponent(orderRef)}/status`,
    )
      .then((res) => {
        setOrderCurrency(res.currency ?? null)
        // Долларовый заказ рубли не подсказывает: подписка продаётся только
        // в рублях, цену показываем общую.
        if (res.currency === 'RUB' && res.subscriptionAmount && res.subscriptionAmount > 0) {
          setPrice(res.subscriptionAmount)
        }
      })
      .catch(() => {
        /* цена по умолчанию */
      })
  }, [sid, orderRef])

  const start = useCallback(
    async (mode: PaymentMode) => {
      setErrorCode(null)
      setLoading(true)
      try {
        const res = await apiPost<PaymentStart & { sid: string; activated?: boolean }>(
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
            ...(orderRef ? { orderRef } : {}),
          },
        )
        if (res.confirmationToken && res.returnUrl && mode === 'embedded') {
          setWidget({ token: res.confirmationToken, returnUrl: res.returnUrl })
          setLoading(false)
          return
        }
        if (res.confirmationUrl) {
          window.location.assign(res.confirmationUrl)
          return
        }
        // Dev-режим без ключей ЮKassa: подписка активирована сразу.
        window.location.assign(
          `/subscribe/return?sid=${encodeURIComponent(res.sid)}&email=${encodeURIComponent(email.trim())}`,
        )
      } catch (err) {
        setErrorCode(err instanceof Error ? err.message : '')
        setLoading(false)
      }
    },
    [email, name, orderRef],
  )

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    void start('embedded')
  }

  // Виджет умеет звать error_callback и fail одним тиком — второй start завёл
  // бы второй платёж, поэтому фолбэк ровно один.
  const fallbackStarted = useRef(false)
  /** Виджет не поднялся — тот же платёж редиректом на страницу ЮKassa. */
  const widgetFail = useCallback(() => {
    if (fallbackStarted.current) return
    fallbackStarted.current = true
    setWidget(null)
    void start('redirect')
  }, [start])

  const priceLabel = price.toLocaleString('ru-RU')

  return (
    <div
      className="min-h-screen"
      style={{ backgroundColor: cream, color: ink, fontFamily: "'Onest', sans-serif" }}
    >
      <style>{`
        @keyframes subFadeUp { from { opacity: 0; transform: translateY(14px); } to { opacity: 1; transform: none; } }
        .sub-reveal { animation: subFadeUp 0.6s ease-out both; }
      `}</style>

      <header className="mx-auto flex max-w-5xl items-center justify-between px-5 py-5">
        <Link
          to={localePath('/', RU)}
          className="text-sm font-bold hover:opacity-70 transition"
          style={{ color: muted }}
        >
          ← На главную
        </Link>
        <Link
          to={localePath('/account', RU)}
          className="text-sm font-bold hover:opacity-70 transition"
          style={{ color: goldDeep }}
        >
          Уже с подпиской? В кабинет
        </Link>
      </header>

      {/* Тёмная hero-панель в духе gold-лендинга */}
      <section
        className="mx-4 md:mx-auto max-w-5xl rounded-3xl overflow-hidden sub-reveal"
        style={{ backgroundColor: inkSoft }}
      >
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
            <span className="font-bold" style={{ color: gold }}>
              {priceLabel} ₽ в месяц
            </span>
            . Отменить можно в любой момент.
          </p>
        </div>
      </section>

      <section
        className="mx-auto max-w-5xl px-4 md:px-0 mt-8 grid gap-3 sm:grid-cols-2 sub-reveal"
        style={{ animationDelay: '0.12s' }}
      >
        {PERKS.map((p) => (
          <div key={p.title} className="rounded-3xl bg-white p-6 shadow-md">
            <p.icon className="h-6 w-6 mb-3" style={{ color: goldDeep }} />
            <div className="text-lg font-bold mb-1.5" style={serif}>
              {p.title}
            </div>
            <p className="text-sm leading-relaxed" style={{ color: muted }}>
              {p.text}
            </p>
          </div>
        ))}
      </section>

      {/* Оформление */}
      <section
        className="mx-auto max-w-xl px-4 md:px-0 mt-10 pb-16 sub-reveal"
        style={{ animationDelay: '0.24s' }}
      >
        <div className="rounded-3xl bg-white p-6 md:p-8 shadow-md">
          <h2 className="text-2xl mb-1" style={serif}>
            Оформление
          </h2>
          <p className="text-sm mb-6" style={{ color: muted }}>
            {priceLabel} ₽ сейчас — и месяц безлимита. Дальше — автоматическое продление раз в месяц, с
            напоминанием на почту за 3 дня до списания.
          </p>

          {foreignOrder && (
            <div
              className="rounded-xl px-4 py-3 text-sm mb-5"
              style={{ backgroundColor: '#fdf3e2', color: goldDeep }}
            >
              Заказ оформлен в {orderCurrency}. Подписка доступна только в рублях — ниже цена в рублях, оплата
              пройдёт через ЮKassa.
            </div>
          )}

          {widget ? (
            <div className="space-y-4">
              <YookassaCheckout
                confirmationToken={widget.token}
                returnUrl={widget.returnUrl}
                onError={widgetFail}
              />
              <p className="text-center text-xs" style={{ color: muted }}>
                Способ оплаты привязывается для продления подписки. Данные карты обрабатывает ЮKassa, мы их не
                видим.
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
                    Я соглашаюсь на автоматическое списание {priceLabel} ₽ раз в месяц с привязанной карты.
                    Отменить подписку можно в любой момент на{' '}
                    <Link to={localePath('/subscription/cancel', RU)} className="underline" style={{ color: goldDeep }}>
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
                    <Link
                      to={localePath('/subscription-offer', RU)}
                      className="underline"
                      style={{ color: goldDeep }}
                    >
                      условия подписки
                    </Link>{' '}
                    и{' '}
                    <Link to={localePath('/privacy', RU)} className="underline" style={{ color: goldDeep }}>
                      политику конфиденциальности
                    </Link>
                    .
                  </span>
                </label>
              </div>

              {errorCode && (
                <div
                  className="rounded-xl px-4 py-3 text-sm"
                  style={{ backgroundColor: '#fdeaea', color: '#8a2121' }}
                >
                  {errorMessage(errorCode)}{' '}
                  {errorCode === 'already_active' && (
                    <Link to={localePath('/account', RU)} className="underline font-semibold">
                      Войти в кабинет
                    </Link>
                  )}
                </div>
              )}

              <button
                type="submit"
                disabled={loading || !recurringConsent || !offerConsent}
                className="w-full rounded-xl px-8 py-4 font-black text-sm transition-all hover:opacity-90 disabled:opacity-40"
                style={{ backgroundColor: ink, color: gold }}
              >
                {loading ? 'Переходим к оплате…' : `Оформить за ${priceLabel} ₽/мес`}
              </button>
              <p className="text-center text-xs" style={{ color: muted }}>
                Оплата через ЮKassa: карта, СБП, SberPay, T-Pay. Способ оплаты привязывается для продления
                подписки.
              </p>
            </form>
          )}
        </div>
      </section>
    </div>
  )
}
