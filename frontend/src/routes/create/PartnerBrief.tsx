/**
 * Анкета песни для заказов, оплаченных у партнёра (Les Roses): песню купили в
 * корзине цветочного магазина, вопросы о получателе там не задавали — их
 * задаёт страница заказа /order/{ref}, пока сервер отвечает needsBrief.
 *
 * Экраны — те же, что в воронке /create (кому / повод / эмоция / история):
 * один набор вопросов на все пути к песне. Отправка — POST /api/orders/{ref}/config,
 * он же запускает генерацию; дальше страница заказа показывает обычный прогресс.
 */

import { useEffect, useState } from 'react'

import { resolveGenre } from '@/flow/types'
import { apiPost } from '@/lib/http'

import { EmotionStep, OccasionStep, RecipientStep, StoryStep } from './steps/SongSteps'
import { EMPTY_FORM, type FlowFormState, type StepProps } from './types'

const STEPS = [RecipientStep, OccasionStep, EmotionStep, StoryStep]
const STEP_IDS = ['recipient', 'occasion', 'emotion', 'story'] as const

export function PartnerBrief({ orderRef, onDone }: { orderRef: string; onDone: () => void }) {
  const [form, setFormState] = useState<FlowFormState>(EMPTY_FORM)
  const [index, setIndex] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Ход анкеты для кабинета партнёра: «открыл ссылку» и «дошёл до шага N».
  // Ответы уходят одним запросом в конце, без этих отметок брошенная на
  // последнем шаге анкета неотличима от неоткрытой. Ошибки молча глотаются.
  useEffect(() => {
    apiPost(`/api/public/order/${encodeURIComponent(orderRef)}/brief-progress`, { step: index + 1 }).catch(() => {})
  }, [orderRef, index])

  const submit = async () => {
    setBusy(true)
    setError(null)
    try {
      await apiPost(`/api/orders/${encodeURIComponent(orderRef)}/config`, {
        recipientName: form.recipientName.trim(),
        relationship: form.relationship,
        occasion: form.occasion,
        story: form.story.trim(),
        emotion: form.emotion,
        musicPreferences: { genre: resolveGenre(form.genre, form.customStyle), language: 'Russian' },
      })
      onDone()
    } catch (e) {
      // Ответы уже приняты (вторая вкладка, двойной клик) — просто к прогрессу.
      if (e instanceof Error && e.message === 'already_processing') {
        onDone()
        return
      }
      setError('Не получилось сохранить ответы. Попробуйте ещё раз.')
    } finally {
      setBusy(false)
    }
  }

  const Step = STEPS[index]
  const props: StepProps = {
    variant: 'v2',
    stepId: STEP_IDS[index],
    stepIndex: index,
    stepTotal: STEPS.length,
    form,
    setForm: (patch) => setFormState((f) => ({ ...f, ...patch })),
    order: {
      orderId: orderRef,
      paymentStatus: 'paid',
      extras: [],
      extraPrices: {},
      paymentAmount: null,
      discountCode: null,
      deliveryHeld: false,
      deliverAt: null,
      subscribed: false,
    },
    price: 0,
    originalPrice: 0,
    onNext: () => {
      if (index < STEPS.length - 1) {
        setIndex(index + 1)
        window.scrollTo({ top: 0 })
      } else if (!busy) {
        void submit()
      }
    },
    onBack: () => setIndex((i) => Math.max(i - 1, 0)),
    canGoBack: index > 0 && !busy,
    // Оплата прошла у партнёра — шаговых отбивок в нашу аналитику воронок нет.
    track: () => {},
    error,
    busy,
  }
  return <Step {...props} />
}
