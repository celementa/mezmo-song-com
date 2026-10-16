/**
 * Рельсы метрик воронки. Здесь только ОТПРАВКА событий в dataLayer единым
 * форматом — теги, цели и ecommerce настраиваются в контейнере GTM (см.
 * ANALYTICS.md) и в этот релиз не входят: сначала функционально готовые шаги,
 * метрики навешиваются после ревью на готовое.
 *
 * Формат события неизменен для всех шагов обоих вариантов:
 *   { event: 'flow_step', flow: 'create', variant: 'v1'|'v2',
 *     step_id: 'style', step_index: 3, step_total: 13, action: 'view'|'next'|… }
 *
 * Этого достаточно, чтобы в GTM построить воронку по step_id и сравнить
 * варианты между собой, не трогая код.
 */

import { reportFlowStep } from '@/lib/flowSteps'
import type { FlowVariant } from './variant'

export type FlowAction = 'view' | 'next' | 'back' | 'skip' | 'submit' | 'error'

type FlowEvent = {
  event: 'flow_step'
  flow: 'create'
  variant: FlowVariant
  step_id: string
  step_index: number
  step_total: number
  action: FlowAction
  /** Свободные поля шага: выбранный стиль, id допника, код ошибки. */
  detail?: Record<string, unknown>
}

function push(payload: FlowEvent): void {
  try {
    const dataLayer = ((window as unknown as { dataLayer?: Record<string, unknown>[] }).dataLayer ??= [])
    dataLayer.push(payload)
  } catch {
    /* аналитика не должна ронять воронку */
  }
}

export type StepTracker = (
  stepId: string,
  stepIndex: number,
  action: FlowAction,
  detail?: Record<string, unknown>,
) => void

/**
 * Трекер, привязанный к варианту и общей длине воронки.
 *
 * arm — плечо A/B-теста, если сервер его назначил: с ним воронку на дашборде
 * можно разрезать по плечам, а не только по варианту.
 */
export function createStepTracker(variant: FlowVariant, stepTotal: number, arm?: string | null): StepTracker {
  return (stepId, stepIndex, action, detail) => {
    // Показ шага дублируется на сервер: дашборд читает SQLite, а не dataLayer.
    if (action === 'view') reportFlowStep(variant, stepId, stepIndex, arm)
    push({
      event: 'flow_step',
      flow: 'create',
      variant,
      step_id: stepId,
      step_index: stepIndex,
      step_total: stepTotal,
      action,
      ...(detail ? { detail } : {}),
    })
  }
}
