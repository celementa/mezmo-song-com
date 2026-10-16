import type { OrderInput } from 'shared'

import { getCurrency, inCurrency, type Currency } from '@/lib/currency'

export const CONFIG_DRAFT_STORAGE_KEY = 'laguin:config_draft:v1'

export type PersistedConfigDraft = {
  v: 1
  draftKey?: string | null
  step: number
  relationship: string
  /** Явный выбор пола получателя для «Друг»/«Другое»; null — не спрашивали или не выбрали. */
  pickedGender?: 'male' | 'female' | null
  emailVerificationId: string | null
  formValues: Partial<OrderInput>
  updatedAt: number
}

export function fmtCurrency(amt: number, currency: Currency = getCurrency()) {
  if (amt === 0) return 'Бесплатно'
  // Суммы приходят в долларах; в рублях — пересчёт по курсу (lib/currency.ts).
  // «$25» / «2 140 ₽».
  return new Intl.NumberFormat(currency === 'RUB' ? 'ru-RU' : 'en-US', {
    style: 'currency',
    currency,
    maximumFractionDigits: 0,
  }).format(inCurrency(amt, currency))
}

/** Russian plural for days (1 день, 2 дня, 5 дней) */
export function ruDaysPhrase(n: number): string {
  const mod10 = n % 10
  const mod100 = n % 100
  let w = 'дней'
  if (mod100 < 11 || mod100 > 14) {
    if (mod10 === 1) w = 'день'
    else if (mod10 >= 2 && mod10 <= 4) w = 'дня'
  }
  return `${n} ${w}`
}

/** Russian plural for hours */
export function ruHoursPhrase(n: number): string {
  const mod10 = n % 10
  const mod100 = n % 100
  let w = 'часов'
  if (mod100 < 11 || mod100 > 14) {
    if (mod10 === 1) w = 'час'
    else if (mod10 >= 2 && mod10 <= 4) w = 'часа'
  }
  return `${n} ${w}`
}

export function getErrorMessage(e: unknown, fallback: string): string {
  if (e instanceof Error && e.message) return e.message
  if (typeof e === 'string') return e
  if (e && typeof e === 'object' && 'message' in e && typeof e.message === 'string') {
    return e.message
  }
  return fallback
}

export function getPersistableFormValues(values: Partial<OrderInput>): Partial<OrderInput> {
  return {
    yourName: values.yourName,
    recipientName: values.recipientName,
    occasion: values.occasion,
    story: values.story,
    musicPreferences: values.musicPreferences,
    email: values.email,
    extraNotes: values.extraNotes,
  }
}

export function hasMeaningfulDraft(values: Partial<OrderInput>): boolean {
  const s = (v: unknown) => String(v ?? '').trim()
  return Boolean(
    s(values.recipientName) ||
      s(values.email) ||
      s(values.story) ||
      s(values.extraNotes)
  )
}

export function generateDraftKey(): string {
  const uuid = globalThis.crypto?.randomUUID?.()
  if (uuid) return uuid
  return `${Date.now().toString(36)}_${Math.random().toString(36).slice(2)}${Math.random().toString(36).slice(2)}`
}

const apiBase = import.meta.env.VITE_API_BASE_URL ?? ''

export function resolveAsset(v: string): string {
  const s = (v ?? '').trim()
  if (!s) return ''
  if (/^https?:\/\//i.test(s)) return s
  return apiBase + s
}
