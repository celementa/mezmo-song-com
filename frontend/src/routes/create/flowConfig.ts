/**
 * Порядок шагов воронки. Оба варианта собраны из ОДНОГО набора шагов —
 * различается только последовательность, поэтому новый порядок для теста
 * добавляется здесь, без правки компонентов шагов.
 *
 * v1 «оплата поздно»: песня создаётся и слушается бесплатно, email и оплата — после.
 * v2 «оплата в начале»: email и оплата первыми, вопросы о песне — после.
 */

import type { FlowVariant } from './variant'

export type StepId =
  | 'recipient'   // кому дарим + имя
  | 'occasion'    // повод
  | 'emotion'     // эмоция песни (жанр — доп. экран внутри шага)
  | 'story'       // история
  | 'generating'  // «Пишем вашу песню…»
  | 'listen'      // прослушивание + шаринг отрывка
  | 'email'       // email-capture (v1)
  | 'upsellSpeed' // ускорение +290 ₽
  | 'payment'     // редирект на страницу ЮKassa (своего экрана нет — v9/v11)
  | 'upsellSub'   // апсейл подписки «Безлимит»
  | 'upsellStems' // инструментал и вокал отдельно +190 ₽
  | 'done'        // готово, скачать MP3

/** Шаги, отвечающие на вопросы о песне — из них собирается payload заказа. */
export const SONG_STEPS: StepId[] = ['recipient', 'occasion', 'emotion', 'story']

const V1_STEPS: StepId[] = [
  'recipient',
  'occasion',
  'emotion',
  'story',
  'generating',
  'listen',
  'email',
  'upsellSpeed',
  'payment',
  'upsellSub',
  'upsellStems',
  'done',
]

const V2_STEPS: StepId[] = [
  'email',
  'payment',
  'recipient',
  'occasion',
  'emotion',
  'story',
  'generating',
  'listen',
  'upsellSpeed',
  'upsellSub',
  'upsellStems',
  'done',
]

export function stepsFor(variant: FlowVariant): StepId[] {
  return variant === 'v2' ? V2_STEPS : V1_STEPS
}

/**
 * Шаги, которые нельзя перемотать назад: после оплаты возврат к вопросам о
 * песне бессмысленен, а «Назад» с прослушивания запустило бы вторую генерацию
 * (и вторую списанную квоту).
 */
export const NO_BACK_STEPS: StepId[] = ['generating', 'listen', 'payment', 'upsellSub', 'upsellStems', 'done']
