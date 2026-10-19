import { getSessionId } from '@/lib/session'

/**
 * Отправка показов шагов воронки на сервер — из этого на дашборде строится
 * «где отпадают люди».
 *
 * Почему не хватает событий в dataLayer: они живут в GTM, а отчёт по воронке
 * читается с дашборда, который смотрит в SQLite приложения. Считать воронку по
 * заказам тоже нельзя — заказ появляется только в её середине (в варианте 1 на
 * шаге email, в /create v1 после истории), и первых шагов в БД нет вовсе.
 *
 * Отправка «пальнул и забыл»: ошибка сети не должна ломать шаг воронки.
 */
const reported = new Set<string>()

export function reportFlowStep(flow: string, stepId: string, stepIndex: number, arm?: string | null): void {
  // Один показ на шаг за загрузку страницы: повторные рендеры и возвраты назад
  // не должны бить в сервер. Дедуп на сервере всё равно есть (UNIQUE по
  // сессии, дню и шагу), но лишние запросы незачем.
  const key = `${flow}:${stepId}`
  if (reported.has(key)) return
  reported.add(key)

  try {
    void fetch('/api/public/flow/step', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId: getSessionId(), flow, arm: arm ?? '', stepId, stepIndex }),
      keepalive: true,
    }).catch(() => {})
  } catch {
    /* приватный режим или заблокированный fetch — воронка работает дальше */
  }
}
