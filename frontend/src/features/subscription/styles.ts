// Оформление страниц подписки: палитра и шрифт перенесены из ai-songs
// (frontend/src/routes/SubscribeRoute.tsx), чтобы оформление, кабинет и отмена
// подписки выглядели одинаково и не расползались по разным задачам.

export const ink = '#151210'
export const inkSoft = '#1d1813'
export const gold = '#F2C572'
export const goldDeep = '#8a6621'
export const muted = '#6b6558'
export const cream = '#f9f4ed'
export const serif = { fontFamily: "'Prata', serif" }

export type SubLang = 'ru' | 'en'

// Единый формат дат подписки на обеих языковых версиях: «13 ноября 2026 г.» /
// «November 13, 2026». Свой Intl.DateTimeFormat на каждый вызов не создаём —
// он дорогой.
const subDateFormatters: Record<SubLang, Intl.DateTimeFormat> = {
  ru: new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' }),
  en: new Intl.DateTimeFormat('en-US', { day: 'numeric', month: 'long', year: 'numeric' }),
}

// null — даты нет (подписка не оформлена, платёж не привязан): показываем прочерк.
export function formatSubDate(iso: string | null, lang: SubLang = 'ru'): string {
  if (!iso) return '—'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return '—'
  return subDateFormatters[lang].format(date)
}
