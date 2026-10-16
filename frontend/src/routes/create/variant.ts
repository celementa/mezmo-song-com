/**
 * A/B-вариант воронки: v1 «оплата поздно» против v2 «оплата в начале».
 *
 * Жребия на фронте НЕТ: вариант назначает сервер через ротацию плеч
 * (/api/public/flow), иначе локальный случайный выбор перебивал бы серверное
 * назначение и ломал сравнимость теста. Здесь только закрепление уже
 * назначенного: `?v=1` / `?v=2` задают вариант принудительно (переход из
 * FlowEntry или ручная проверка) и запоминаются в localStorage — человек
 * видит свой вариант, даже вернувшись через неделю.
 */

export type FlowVariant = 'v1' | 'v2'

const KEY = '_flow_variant'

function read(): FlowVariant | null {
  try {
    const raw = localStorage.getItem(KEY)
    return raw === 'v1' || raw === 'v2' ? raw : null
  } catch {
    return null // приватный режим — storage недоступен
  }
}

function write(v: FlowVariant): void {
  try {
    localStorage.setItem(KEY, v)
  } catch {
    /* приватный режим — вариант живёт до перезагрузки */
  }
}

/** Явный вариант из URL (?v=1 / ?v=2), если задан. */
export function variantFromSearch(search: string): FlowVariant | null {
  const v = new URLSearchParams(search).get('v')
  if (v === '1' || v === 'v1') return 'v1'
  if (v === '2' || v === 'v2') return 'v2'
  return null
}

/**
 * Возвращает вариант этого посетителя: явный из URL или закреплённый ранее.
 * null — вариант ещё не назначен; вызывающий спрашивает плечо у сервера
 * (CreateRoute → useFlowConfig), фронт сам ничего не разыгрывает.
 */
export function resolveVariant(search = window.location.search): FlowVariant | null {
  const forced = variantFromSearch(search)
  if (forced) {
    write(forced)
    return forced
  }
  return read()
}
