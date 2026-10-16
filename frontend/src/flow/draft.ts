import type { FlowAnswers } from './types'

/**
 * Черновик воронки варианта 1 (/config) в localStorage.
 *
 * Состояние воронки жило только в памяти React: уход в приложение банка из
 * виджета ЮKassa и возврат с перезагрузкой, как и кнопка «назад» браузера со
 * страницы заказа, начинали анкету с первого вопроса — и заводили второй
 * заказ с новым платёжом. Черновик восстанавливает ответы, заказ и шаг;
 * сам шаг при этом живёт в адресе (`?s=N`), чтобы «назад»/«вперёд» браузера
 * ходили по шагам, а не выкидывали из воронки.
 */
const KEY = 'mezmo:flow-draft:v1'
const TTL_MS = 24 * 60 * 60 * 1000

export type FlowDraft = {
  v: 1
  /** Технический id плеча: черновик другого плеча (другая цена и шаги) не восстанавливаем. */
  flowVariant: string
  answers: FlowAnswers
  orderRef: string | null
  stepIndex: number
  payVia: 'order' | 'subscription'
  /** Заказ черновика уже оплачен: восстанавливаем только по истории браузера, не при новом заходе. */
  paid?: boolean
  updatedAt: number
}

export function loadFlowDraft(flowVariant: string): FlowDraft | null {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return null
    const d = JSON.parse(raw) as FlowDraft
    if (d.v !== 1 || d.flowVariant !== flowVariant) return null
    if (typeof d.updatedAt !== 'number' || Date.now() - d.updatedAt > TTL_MS) return null
    return d
  } catch {
    return null
  }
}

export function saveFlowDraft(draft: Omit<FlowDraft, 'v' | 'updatedAt'>) {
  try {
    localStorage.setItem(KEY, JSON.stringify({ ...draft, v: 1, updatedAt: Date.now() }))
  } catch {
    // приватный режим / квота — воронка работает и без черновика
  }
}

export function clearFlowDraft() {
  try {
    localStorage.removeItem(KEY)
  } catch {
    // см. saveFlowDraft
  }
}

/** Номер шага из адреса (`?s=N`); null — параметра нет. */
export function stepFromSearch(search: string): number | null {
  const raw = new URLSearchParams(search).get('s')
  if (raw === null) return null
  const n = Number(raw)
  return Number.isInteger(n) && n >= 0 ? n : null
}

/** Тот же адрес с другим номером шага; остальные параметры (`arm`) сохраняются. */
export function searchWithStep(search: string, step: number): string {
  const params = new URLSearchParams(search)
  params.set('s', String(step))
  return `?${params.toString()}`
}
