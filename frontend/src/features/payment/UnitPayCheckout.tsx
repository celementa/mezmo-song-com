/**
 * Шаг оплаты mezmo-song.com: сумма, обязательное согласие с офертой
 * (требование банка) и переход на платёжную форму UnitPay.
 *
 * Сервер заводит платёж (initPayment) только по кнопке, а не при показе шага:
 * без принятой оферты платить нельзя. Ответ — widgetUrl: та же форма UnitPay
 * открывается окном поверх сайта (UnitPayWidget), после оплаты покупатель
 * уходит на returnUrl. Нет widgetUrl — переход на форму по confirmationUrl,
 * откуда UnitPay возвращает на resultUrl, который задал сервер.
 */

import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ShieldCheck } from 'lucide-react'

import { StepShell } from '@/routes/create/StepShell'
import { formatMoney, type OrderCurrency } from '@/lib/currency'
import { localePath, useTr } from '@/lib/lang'
import { PaymentLogos } from '@/components/landing/PaymentLogos'
import { loadStripeJs } from './loadStripe'
import { loadYookassaWidget } from './loadYookassaWidget'

export type PaymentMode = 'embedded' | 'redirect'

/** Ответ любой ручки, заводящей платёж (initiate-payment и т. п.). */
export type PaymentStart = {
  /** Кто выставил счёт: от него зависит, какую форму показывать. */
  provider?: 'yookassa' | 'stripe' | 'unitpay'
  confirmationUrl?: string
  /** Форма UnitPay для открытия во iframe и куда вести после оплаты в нём. */
  widgetUrl?: string
  returnUrl?: string
  /** Встроенная форма Stripe: client_secret и публичный ключ. */
  clientSecret?: string
  publishableKey?: string
  sessionId?: string
  /** Виджет ЮKassa: confirmation_token из платежа и адрес возврата. */
  confirmationToken?: string
  paymentId?: string
  /** Оплата не нужна: цена 0 или mock-режим стенда. */
  paid?: boolean
  activated?: boolean
}

/** Заказ уже оплачен — уведомление UnitPay просто не доехало до сервера. */
const SETTLED_ERRORS = ['already_paid', 'not_pending']

/**
 * Текст на отказ сервера. Отказы разных причин раньше выглядели одинаково
 * («Не получилось перейти к оплате»), и человек не понимал, что именно
 * произошло и что делать дальше.
 */
function paymentErrorText(msg: string, tr: (ru: string, en: string) => string): string {
  if (msg === 'subscriptions_unavailable') {
    return tr(
      'Подписку нельзя оформить этим способом оплаты. Вернитесь назад и выберите оплату одной песни.',
      'A subscription cannot be purchased with this payment method. Go back and choose a one-time song.',
    )
  }
  if (msg === 'price_unavailable') {
    return tr(
      'Цена для этого способа оплаты не настроена. Попробуйте другой способ.',
      'The price for this payment method is not configured. Please choose another method.',
    )
  }
  if (msg === 'payments_unavailable') {
    return tr(
      'Оплата этим способом временно недоступна. Попробуйте другой способ или повторите через пару минут.',
      'This payment method is temporarily unavailable. Try another one or retry in a couple of minutes.',
    )
  }
  return tr('Не получилось перейти к оплате. Попробуйте ещё раз.', "Couldn't open payment. Please try again.")
}

export function UnitPayCheckout({
  shell,
  amount,
  currency,
  provider = 'unitpay',
  original,
  perMonth = false,
  summary,
  start,
  onPaid,
}: {
  shell: { stepIndex: number; stepTotal: number; onBack: () => void; canGoBack: boolean }
  /** Сумма в валюте заказа: рубли или доллары. */
  amount: number
  currency: OrderCurrency
  /** Провайдер ветки: ЮKassa/Stripe/UnitPay. */
  provider?: 'yookassa' | 'stripe' | 'unitpay'
  original?: number | null
  perMonth?: boolean
  /** Что покупается: кому, повод, куда придёт песня. */
  summary?: ReactNode
  start: (mode: PaymentMode) => Promise<PaymentStart>
  onPaid: () => void
}) {
  const tr = useTr()
  const money = (value: number) => formatMoney(value, currency) + (perMonth ? tr('/мес', '/mo') : '')
  const [agreed, setAgreed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [widget, setWidget] = useState<{ url: string; returnUrl: string } | null>(null)
  const [stripe, setStripe] = useState<{ clientSecret: string; publishableKey: string } | null>(null)
  // Виджет ЮKassa живёт прямо на странице — человек не уходит на чужой домен и,
  // значит, не зависит от того, вернёт ли его банковское приложение обратно.
  const [yoo, setYoo] = useState<{ token: string; returnUrl: string } | null>(null)

  const pay = async () => {
    if (!agreed) {
      setError(
        tr(
          'Чтобы перейти к оплате, примите условия Пользовательского соглашения.',
          'To continue to payment, please accept the Terms of Use.',
        ),
      )
      return
    }
    setError(null)
    setBusy(true)
    try {
      // ЮKassa и Stripe показывают свою форму прямо на сайте; UnitPay —
      // редиректом (в тестовом режиме) либо своим виджетом (widgetUrl).
      const res = await start(provider === 'stripe' || provider === 'yookassa' ? 'embedded' : 'redirect')
      if ((res.provider === 'stripe' || provider === 'stripe') && res.clientSecret) {
        if (!res.publishableKey) {
          setError(tr('Stripe не настроен. Попробуйте другой способ оплаты.', 'Stripe is not configured. Please choose another payment method.'))
          return
        }
        setStripe({ clientSecret: res.clientSecret, publishableKey: res.publishableKey })
        return
      }
      if (res.confirmationToken && res.returnUrl) {
        setYoo({ token: res.confirmationToken, returnUrl: res.returnUrl })
        return
      }
      if (res.widgetUrl && res.returnUrl) {
        setWidget({ url: res.widgetUrl, returnUrl: res.returnUrl })
        return
      }
      if (res.confirmationUrl) {
        window.location.assign(res.confirmationUrl)
        return
      }
      if (res.paid || res.activated) {
        onPaid()
        return
      }
      setError(tr('Не получилось перейти к оплате. Попробуйте ещё раз.', "Couldn't open payment. Please try again."))
    } catch (e) {
      const msg = e instanceof Error ? e.message : ''
      if (SETTLED_ERRORS.includes(msg)) {
        onPaid()
        return
      }
      setError(paymentErrorText(msg, tr))
    }
    setBusy(false)
  }

  const linkClass = 'font-semibold text-[#8a6621] underline underline-offset-2 hover:text-[#151210]'

  return (
    <StepShell
      {...shell}
      busy={busy}
      error={error}
      eyebrow={tr('Оплата', 'Payment')}
      headline={tr('Ваш заказ', 'Your order')}
      subhead={tr(
        'Персональная песня по вашей истории: аудиофайл и текст, доступ навсегда.',
        'A personal song based on your story: audio file and lyrics, lifetime access.',
      )}
      primaryLabel={
        stripe || yoo
          ? tr('Форма оплаты открыта ниже', 'Payment form is open below')
          : busy
            ? tr('Открываем оплату…', 'Opening payment…')
            : tr(`Оплатить ${money(amount)}`, `Pay ${money(amount)}`)
      }
      onPrimary={() => void pay()}
      primaryDisabled={!agreed || busy || Boolean(stripe) || Boolean(yoo)}
    >
      <div className="rounded-2xl border border-[#EFE4D2] bg-white p-4">
        <div className="flex items-baseline justify-between gap-3">
          <span className="text-sm text-[#6b6259]">{tr('К оплате', 'Total')}</span>
          <span className="text-2xl font-bold text-[#151210]">
            {money(amount)}
            {original && original > amount ? (
              <span className="ml-2 text-sm font-normal text-[#b4a893] line-through">{formatMoney(original, currency)}</span>
            ) : null}
          </span>
        </div>
        {summary ? <div className="mt-3 text-xs text-[#6b6259]">{summary}</div> : null}
      </div>

      <label className="mt-4 flex cursor-pointer items-start gap-3 text-xs leading-relaxed text-[#3b352f]">
        <input
          type="checkbox"
          checked={agreed}
          onChange={(e) => {
            setAgreed(e.target.checked)
            setError(null)
          }}
          className="mt-0.5 h-4 w-4 shrink-0 accent-[#B9852E]"
        />
        <span>
          {tr('Я принимаю', 'I accept the')}{' '}
          <Link to={localePath('/terms')} target="_blank" className={linkClass}>
            {tr('Пользовательское соглашение', 'Terms of Use')}
          </Link>{' '}
          ({tr('включая', 'including the')}{' '}
          <Link to={localePath('/security')} target="_blank" className={linkClass}>
            {tr('Политику информационной безопасности', 'Information Security Policy')}
          </Link>
          ) {tr('и даю', 'and give')}{' '}
          <Link to={localePath('/consent')} target="_blank" className={linkClass}>
            {tr('согласие на обработку персональных данных', 'consent to the processing of personal data')}
          </Link>{' '}
          {tr('в соответствии с', 'in accordance with the')}{' '}
          <Link to={localePath('/privacy')} target="_blank" className={linkClass}>
            {tr('Политикой конфиденциальности', 'Privacy Policy')}
          </Link>
          .
        </span>
      </label>

      <p className="mt-4 text-xs text-[#6b6259]">
        {provider === 'stripe'
          ? tr('Оплата картой проходит на защищённой форме Stripe прямо на сайте.', 'Card payment is processed in Stripe’s secure form right on this site.')
          : provider === 'yookassa'
            ? tr('Оплата картой или через СБП проходит на защищённой форме ЮKassa прямо на сайте.', 'Card or SBP payment is processed in YooKassa’s secure form right on this site.')
            : tr('Оплата проходит в защищённом окне UnitPay прямо на сайте.', 'Payment is processed in UnitPay’s secure window right on this site.')}
      </p>
      <div className="mt-2 flex flex-wrap items-center justify-between gap-3">
        <p className="inline-flex items-center gap-1.5 text-xs text-[#9c9186]">
          <ShieldCheck className="h-3.5 w-3.5" />{' '}
          {tr('Данные карты мы не видим — их обрабатывает платёжный провайдер (PCI DSS).', 'We never see your card details — they are handled by the payment provider (PCI DSS).')}
        </p>
        {provider === 'stripe' ? <StripeLogos /> : provider === 'yookassa' ? <RuLogos /> : <PaymentLogos />}
      </div>
      {yoo ? (
        <YookassaCheckoutWidget
          confirmationToken={yoo.token}
          returnUrl={yoo.returnUrl}
          onError={() =>
            setError(
              tr(
                'Не получилось открыть форму ЮKassa. Попробуйте ещё раз или выберите другой способ оплаты.',
                "Couldn't open the YooKassa form. Please try again or choose another payment method.",
              ),
            )
          }
        />
      ) : null}
      {stripe ? (
        <StripeEmbeddedCheckout
          clientSecret={stripe.clientSecret}
          publishableKey={stripe.publishableKey}
          onError={() => setError(tr('Не получилось открыть форму Stripe. Попробуйте ещё раз.', "Couldn't open the Stripe form. Please try again."))}
        />
      ) : null}
      {widget ? (
        <UnitPayWidget
          url={widget.url}
          onSuccess={() => window.location.assign(widget.returnUrl)}
          onClose={() => {
            setWidget(null)
            setBusy(false)
          }}
        />
      ) : null}
    </StepShell>
  )
}

const WIDGET_ORIGIN = 'https://widget.unitpay.money'

/**
 * Платёжный виджет UnitPay: их форма во iframe на весь экран. Делает то же,
 * что их widget.js, но без него: тот вешает глобальный window.onmessage и не
 * проверяет, от кого пришло сообщение. Форма шлёт родителю JSON-строки
 * {action: 'success' | 'close' | ...}. Оплату засчитывает только уведомление
 * UnitPay серверу — 'success' лишь уводит на страницу заказа, которая ждёт его.
 */
/** Встроенная форма Stripe: карты, Apple Pay и Google Pay показывает она сама. */
export function StripeEmbeddedCheckout({
  clientSecret,
  publishableKey,
  onError,
}: {
  clientSecret: string
  publishableKey: string
  onError: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  // Колбэк ошибки не должен пересоздавать форму: родитель передаёт inline-функцию,
  // её идентичность меняется на каждом рендере, и useEffect с onError в зависимостях
  // уничтожал бы и монтировал Stripe заново — форма «мигала» бесконечно.
  const onErrorRef = useRef(onError)
  useEffect(() => {
    onErrorRef.current = onError
  }, [onError])
  useEffect(() => {
    let alive = true
    let instance: { destroy: () => void } | null = null
    void loadStripeJs()
      .then(() => {
        const factory = window.Stripe
        if (!factory) throw new Error('stripe_js_missing')
        return factory(publishableKey).initEmbeddedCheckout({ clientSecret })
      })
      .then((checkout) => {
        if (!alive || !ref.current) {
          checkout.destroy()
          return
        }
        checkout.mount(ref.current)
        instance = checkout
        // Форма Stripe монтируется ниже сводки и юридического текста; на телефоне
        // её легко не заметить за cookie-баннером. Подводим экран к форме.
        window.setTimeout(() => ref.current?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 50)
        // Форма Stripe монтируется внизу шага, и её легко не заметить — особенно
        // на телефоне и под баннером cookie. Подводим экран к ней сразу после
        // отрисовки: иначе покупатель видит только сводку и галочку.
        ref.current.scrollIntoView({ behavior: 'smooth', block: 'center' })
      })
      .catch(() => {
        if (alive) onErrorRef.current()
      })
    return () => {
      alive = false
      instance?.destroy()
    }
  }, [clientSecret, publishableKey])
  return <div ref={ref} className="mt-4 min-h-[320px]" />
}

/**
 * Виджет ЮKassa: карта, СБП, SberPay, ЮMoney — прямо на нашей странице, без
 * ухода на чужой домен. Поэтому возврат с оплаты перестаёт зависеть от того,
 * вернёт ли человека приложение банка: страница заказа сама опрашивает статус,
 * а кнопка «Оплатить» остаётся на месте.
 */
export function YookassaCheckoutWidget({
  confirmationToken,
  returnUrl,
  onError,
}: {
  confirmationToken: string
  returnUrl: string
  onError: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  // Виджет рендерится по id контейнера, а не по ссылке на узел.
  const containerId = useRef(`yookassa-widget-${Math.random().toString(36).slice(2)}`)
  const onErrorRef = useRef(onError)
  useEffect(() => {
    onErrorRef.current = onError
  }, [onError])
  useEffect(() => {
    let alive = true
    let widget: { destroy: () => void } | null = null
    void loadYookassaWidget()
      .then(() => {
        const Ctor = window.YooMoneyCheckoutWidget
        if (!Ctor) throw new Error('yookassa_widget_missing')
        const instance = new Ctor({
          confirmation_token: confirmationToken,
          return_url: returnUrl,
          error_callback: () => {
            if (alive) onErrorRef.current()
          },
        })
        // success — платёж прошёл: уводим на страницу заказа. Засчитывает оплату
        // только уведомление ЮKassa (или сверка), страница его дождётся.
        instance.on('success', () => window.location.assign(returnUrl))
        instance.on('fail', () => {
          if (alive) onErrorRef.current()
        })
        return instance.render(containerId.current).then(() => {
          if (!alive) {
            instance.destroy()
            return
          }
          widget = instance
          // Форма монтируется внизу шага — подводим к ней экран, как у Stripe.
          ref.current?.scrollIntoView({ behavior: 'smooth', block: 'center' })
        })
      })
      .catch(() => {
        if (alive) onErrorRef.current()
      })
    return () => {
      alive = false
      widget?.destroy()
    }
  }, [confirmationToken, returnUrl])
  return <div id={containerId.current} ref={ref} className="mt-4 min-h-[420px]" />
}

function RuLogos() {
  return (
    <div className="flex flex-wrap items-center gap-1.5 text-[10px] font-semibold text-[#5d554b]">
      {['Мир', 'СБП', 'SberPay', 'T-Pay', 'ЮMoney'].map((m) => (
        <span key={m} className="rounded-md border border-[#E7DAC5] bg-white px-1.5 py-0.5">{m}</span>
      ))}
    </div>
  )
}

function StripeLogos() {
  return (
    <div className="flex flex-wrap items-center gap-1.5 text-[10px] font-semibold text-[#5d554b]">
      {['Visa', 'Mastercard', 'Apple Pay', 'Google Pay'].map((m) => (
        <span key={m} className="rounded-md border border-[#E7DAC5] bg-white px-1.5 py-0.5">{m}</span>
      ))}
    </div>
  )
}

function UnitPayWidget({ url, onSuccess, onClose }: { url: string; onSuccess: () => void; onClose: () => void }) {
  const tr = useTr()
  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if (e.origin !== WIDGET_ORIGIN || typeof e.data !== 'string') return
      let action: unknown
      try {
        action = (JSON.parse(e.data) as { action?: unknown }).action
      } catch {
        return
      }
      if (action === 'success') onSuccess()
      if (action === 'close') onClose()
    }
    window.addEventListener('message', onMessage)
    document.body.classList.add('overflow-hidden')
    return () => {
      window.removeEventListener('message', onMessage)
      document.body.classList.remove('overflow-hidden')
    }
  }, [onSuccess, onClose])

  return (
    <div className="fixed inset-0 z-[99999] bg-black/60">
      <iframe src={url} title={tr('Оплата UnitPay', 'UnitPay payment')} allow="payment" className="h-full w-full border-0 bg-transparent" />
    </div>
  )
}
