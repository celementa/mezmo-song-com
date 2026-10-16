/**
 * Коммерческие шаги воронки: апсейл ускорения, переход к оплате ЮKassa,
 * апсейл подписки, апсейл стемов и финал.
 *
 * Экрана сводки заказа (с промокодом) нет с v11/v12 документа: шаг payment —
 * это виджет ЮKassa на своём домене (v20), редирект на страницу ЮKassa — фолбэк.
 * Апсейлы до оплаты добавляют свою цену в основной платёж, после оплаты
 * оформляются отдельным платежом.
 */

import { useEffect, useRef } from 'react'
import { Check, Download, Loader2, PartyPopper, ShieldCheck, Zap } from 'lucide-react'

import { GOLD, StepShell } from '../StepShell'
import { YooKassaWidget } from '@/features/payment/YooKassaWidget'
import { useEmbeddedPayment, type PaymentMode, type PaymentStart } from '@/features/payment/useEmbeddedPayment'
import { shellProps, type StepProps } from '../types'
import { reportEngagement } from '@/lib/engagement'

const rub = (n: number) => `${n.toLocaleString('ru-RU')} ₽`

/** Время доставки в человеческом виде; мусор в дате не должен ломать экран. */
function formatTime(iso: string): string {
  const at = new Date(iso)
  return Number.isNaN(at.getTime()) ? 'скоро' : at.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
}

/** Апсейл ускорения. До оплаты — в основной платёж, после — доплата. */
export function UpsellSpeedStep(p: StepProps & { onAccept: () => void; afterPayment: boolean }) {
  const price = p.order.extraPrices.speed ?? 0
  const already = p.order.extras.includes('speed')

  // Цена 0 = допник не продаётся: либо выключен в админке, либо выполнять его
  // нечем (в воронке v2 задержки доставки нет, а ускорение продаёт именно её
  // снятие). Тогда шаг просто пропускается — предлагать то, что не будет
  // выполнено, нельзя.
  const disabled = Object.keys(p.order.extraPrices).length > 0 && price <= 0
  useEffect(() => {
    if (disabled) p.onNext()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [disabled])
  if (disabled) return null

  return (
    <StepShell
      {...shellProps(p)}
      canGoBack={p.canGoBack && !p.afterPayment}
      eyebrow={p.afterPayment ? 'Дополнительно' : 'Перед оплатой'}
      headline="Хотите получить песню быстрее?"
      subhead={`Обычно песня готова за 2 часа. ${p.afterPayment ? 'Доплатите' : 'Добавьте ускорение'} — и получите её за 10–30 минут.`}
      primaryLabel={already ? 'Далее' : `Добавить за ${rub(price)}`}
      onPrimary={() => {
        if (already) {
          p.onNext()
          return
        }
        p.track(p.stepId, p.stepIndex, 'submit', { extra: 'speed', price })
        p.onAccept()
      }}
      secondaryLabel={already ? undefined : 'Не нужно'}
      onSecondary={() => {
        p.track(p.stepId, p.stepIndex, 'skip', { extra: 'speed' })
        p.onNext()
      }}
    >
      <div className="rounded-2xl border border-[#EFE4D2] bg-[#FDFBF7] p-4">
        <div className="flex items-center gap-3">
          <span className="flex h-10 w-10 items-center justify-center rounded-full" style={{ background: GOLD }}>
            <Zap className="h-5 w-5 text-[#151210]" />
          </span>
          <div>
            <div className="text-sm font-bold text-[#151210]">+{rub(price)}</div>
            <div className="text-xs text-[#6b6259]">
              {p.afterPayment ? 'Отдельный платёж' : `итого ${rub(p.price + price)}`}
            </div>
          </div>
        </div>
      </div>
      {already ? (
        <p className="mt-3 inline-flex items-center gap-1.5 text-sm font-medium text-[#2f7a45]">
          <Check className="h-4 w-4" /> Ускорение добавлено к заказу
        </p>
      ) : null}
    </StepShell>
  )
}

/**
 * Оплата: виджет ЮKassa прямо на шаге; редирект на страницу ЮKassa — только
 * если виджет не поднялся. После оплаты виджет уводит на returnUrl сервера
 * (/create?order=…&paid=1), и воронка продолжается со следующего шага.
 */
export function PaymentStep(p: StepProps & { start: (mode: PaymentMode) => Promise<PaymentStart>; onPaid: () => void }) {
  const extrasSum = p.order.extras.reduce((sum, id) => sum + (p.order.extraPrices[id] ?? 0), 0)
  const total = p.order.paymentAmount ?? p.price + extrasSum
  const tracked = useRef(false)
  useEffect(() => {
    if (tracked.current) return
    tracked.current = true
    p.track(p.stepId, p.stepIndex, 'submit', { total, auto: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  const { state, widgetFail, retry } = useEmbeddedPayment({ start: p.start, onPaid: p.onPaid })

  return (
    <StepShell
      {...shellProps(p)}
      canGoBack={false}
      error={state.status === 'error' ? state.message : null}
      eyebrow="Оплата"
      headline={state.status === 'error' ? 'Не получилось перейти к оплате' : `Оплата ${rub(total)}`}
      primaryLabel={state.status === 'error' ? `Оплатить ${rub(total)}` : undefined}
      onPrimary={state.status === 'error' ? retry : undefined}
      hideNav={state.status !== 'error'}
    >
      {state.status === 'widget' ? (
        <YooKassaWidget confirmationToken={state.token} returnUrl={state.returnUrl} onFail={widgetFail} />
      ) : state.status === 'error' ? null : (
        <div className="flex flex-col items-center py-6 text-center">
          <Loader2 className="h-8 w-8 animate-spin text-[#B9852E]" />
          <p className="mt-4 text-sm text-[#6b6259]">
            {state.status === 'redirecting' ? 'Переходим на страницу ЮKassa…' : 'Готовим форму оплаты…'}
          </p>
        </div>
      )}
      <p className="mt-3 inline-flex items-center gap-1.5 text-xs text-[#9c9186]">
        <ShieldCheck className="h-3.5 w-3.5" /> Данные карты обрабатывает ЮKassa, мы их не видим
      </p>
    </StepShell>
  )
}

/**
 * Апсейл подписки сразу после оплаты — «0 ₽ сегодня» (v11): оплаченная песня
 * и есть первый месяц, карта уже сохранена платежом за неё, списание через
 * месяц. Если карта не сохранилась (кошелёк без сохранения), сервер отвечает
 * no_saved_method — тогда кнопка ведёт на обычное оформление /subscribe.
 */
export function UpsellSubscriptionStep(
  p: StepProps & { subscriptionPrice: number; subscribed: boolean; onSubscribe: () => void },
) {
  return (
    <StepShell
      {...shellProps(p)}
      canGoBack={false}
      eyebrow="Спасибо за оплату"
      headline="Ваша песня оплачена — но у нас есть идея получше"
      subhead="За те же деньги вы можете получать неограниченное число песен каждый месяц, а не одну разово."
      primaryLabel={p.subscribed ? 'Далее' : 'Перейти на безлимит за 0 ₽'}
      onPrimary={() => {
        if (p.subscribed) {
          p.onNext()
          return
        }
        p.track(p.stepId, p.stepIndex, 'submit', { price: 0, monthly: p.subscriptionPrice })
        p.onSubscribe()
      }}
      secondaryLabel={p.subscribed ? undefined : 'Не сейчас'}
      onSecondary={() => {
        p.track(p.stepId, p.stepIndex, 'skip')
        p.onNext()
      }}
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="rounded-2xl border border-[#EFE4D2] bg-white p-4">
          <div className="text-[11px] font-bold uppercase tracking-[0.14em] text-[#9c9186]">То, что вы купили</div>
          <div className="mt-1.5 text-xl font-bold text-[#151210]">{rub(p.price)}</div>
          <div className="text-xs text-[#9c9186]">разово, 1 песня</div>
          <ul className="mt-3 space-y-1.5 text-xs text-[#6b6259]">
            <li>• 1 готовая песня</li>
            <li>• Доступ навсегда</li>
          </ul>
        </div>
        <div className="relative rounded-2xl border-2 border-[#e3c98f] bg-[#FCF6EA] p-4">
          <span
            className="absolute -top-2.5 right-3 rounded-full px-2 py-0.5 text-[10px] font-bold text-[#151210]"
            style={{ background: GOLD }}
          >
            Выгоднее
          </span>
          <div className="text-[11px] font-bold uppercase tracking-[0.14em] text-[#8a6621]">Безлимит</div>
          <div className="mt-1.5 text-xl font-bold text-[#151210]">0 ₽</div>
          <div className="text-xs text-[#9c9186]">сегодня, без ограничений</div>
          <ul className="mt-3 space-y-1.5 text-xs text-[#6b6259]">
            <li>• Неограниченное число песен</li>
            <li>• Отмена в любой момент</li>
            <li>• Сегодняшняя песня уже включена</li>
          </ul>
        </div>
      </div>
      {p.subscribed ? (
        <p className="mt-3 inline-flex items-center gap-1.5 text-sm font-medium text-[#2f7a45]">
          <Check className="h-4 w-4" /> Подписка оформлена — письмо с деталями уже у вас
        </p>
      ) : (
        <p className="mt-3 text-center text-xs text-[#9c9186]">
          Далее — {rub(p.subscriptionPrice)}/мес, спишем через месяц. Отмена в любой момент.
        </p>
      )}
    </StepShell>
  )
}

/** Апсейл стемов — доплата после оплаты песни. */
export function UpsellStemsStep(p: StepProps & { onAccept: () => void }) {
  // Как и у ускорения: нулевая цена значит «отдать нечем» (разделение дорожек
  // у Kie.ai не подключено), и шаг пропускается.
  const stemsDisabled = Object.keys(p.order.extraPrices).length > 0 && (p.order.extraPrices.stems ?? 0) <= 0
  useEffect(() => {
    if (stemsDisabled) p.onNext()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stemsDisabled])
  if (stemsDisabled) return null
  const price = p.order.extraPrices.stems ?? 190
  const already = p.order.extras.includes('stems')

  return (
    <StepShell
      {...shellProps(p)}
      canGoBack={false}
      eyebrow="Пока песня свежая"
      headline="Добавить инструментал и вокал отдельно?"
      subhead="Отдельные дорожки — чтобы спеть самому под минус или смонтировать видео."
      // Допник уже куплен (возврат с отдельного платежа): единственная дорога
      // дальше — «Далее», а не «Не нужно» под только что оплаченным.
      primaryLabel={already ? 'Далее' : `Да, добавить за ${rub(price)}`}
      onPrimary={() => {
        if (already) {
          p.onNext()
          return
        }
        p.track(p.stepId, p.stepIndex, 'submit', { extra: 'stems', price })
        p.onAccept()
      }}
      secondaryLabel={already ? undefined : 'Не нужно'}
      onSecondary={() => {
        p.track(p.stepId, p.stepIndex, 'skip', { extra: 'stems' })
        p.onNext()
      }}
    >
      <div className="rounded-2xl border border-[#EFE4D2] bg-[#FDFBF7] p-4 text-sm text-[#6b6259]">
        Отдельный платёж на {rub(price)} — вернёмся сюда сразу после оплаты.
      </div>
      {already ? (
        <p className="mt-3 inline-flex items-center gap-1.5 text-sm font-medium text-[#2f7a45]">
          <Check className="h-4 w-4" /> Дорожки добавлены к заказу
        </p>
      ) : null}
    </StepShell>
  )
}

/** Финал: скачать MP3 и перейти на страницу заказа. */
export function DoneStep(p: StepProps & { tracks: string[]; orderUrl: string; onRestart: () => void }) {
  const track = p.tracks[0] ?? ''
  return (
    <StepShell {...shellProps(p)} canGoBack={false} eyebrow="Готово" hideNav>
      <div className="flex flex-col items-center py-4 text-center">
        <PartyPopper className="h-10 w-10 text-[#B9852E]" />
        <h2 className="mt-4 font-[Prata,serif] text-[24px] text-[#151210]">Ваша песня готова!</h2>
        <p className="mt-2 text-sm text-[#6b6259]">
          Копию отправили на почту{p.form.email ? ` ${p.form.email}` : ''} — не потеряется.
        </p>

        {p.order.deliveryHeld ? (
          // Доставка придержана: полный трек прокси пока не отдаёт (и не должен —
          // именно снятие этого ожидания продаёт ускорение). Кнопка «Скачать»
          // здесь вернула бы 404, поэтому вместо неё честный срок.
          <p className="mt-6 rounded-2xl border border-[#EFE4D2] bg-[#FDFBF7] px-4 py-3 text-sm leading-relaxed text-[#6b6259]">
            Песня готовится. Пришлём её на email
            {p.order.deliverAt ? ` примерно к ${formatTime(p.order.deliverAt)}` : ' совсем скоро'} — страница заказа
            обновится сама.
          </p>
        ) : track ? (
          <a
            href={track}
            download
            onClick={() => {
              p.track(p.stepId, p.stepIndex, 'submit', { action: 'download' })
              reportEngagement(p.order.orderId, 'download')
            }}
            className="mt-6 inline-flex items-center gap-2 rounded-xl px-6 py-3 text-sm font-bold text-[#151210] shadow-[0_10px_24px_-14px_rgba(185,133,46,0.9)] transition active:scale-[0.98]"
            style={{ background: `linear-gradient(135deg,#f6dfa3 0%, ${GOLD} 40%, #B9852E 100%)` }}
          >
            <Download className="h-4 w-4" />
            Скачать MP3
          </a>
        ) : null}

        <a href={p.orderUrl} className="mt-4 text-sm font-medium text-[#8a6621] underline underline-offset-4">
          Открыть страницу заказа
        </a>

        {/* Вторая песня начинается отсюда: письмо-догонка с промокодом ведёт на
            /create, и без этой кнопки человек попадал на этот же экран со своим
            прошлым заказом — купить ещё одну было нельзя. */}
        <button
          type="button"
          onClick={() => {
            p.track(p.stepId, p.stepIndex, 'submit', { action: 'restart' })
            p.onRestart()
          }}
          className="mt-3 text-sm font-medium text-[#6b6259] underline underline-offset-4"
        >
          Создать ещё одну песню
        </button>
      </div>
    </StepShell>
  )
}
