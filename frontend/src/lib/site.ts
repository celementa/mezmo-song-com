// Реквизиты юрлиц mezmo-song.com — единственное место, откуда их берут футер,
// контакты и юридические страницы.
//
// Юрлицо зависит от платёжной ветки, а не от языка страницы: рублёвые платежи
// идут на российское ООО «МБК» (ЮKassa), международные — на Celementa FZCO
// (Stripe или UnitPay). Русские юрдоки (src/legal/*.md) описывают МБК,
// английские (src/legal/en/*.md) — FZCO. Без выбранной ветки (лендинг) пара
// «язык страницы → документы» остаётся прежней.
import { localePath } from './lang'

export type Company = {
  name: string
  /** Регистрационный номер иностранного юрлица (FZCO). */
  regNo?: string
  inn?: string
  ogrn?: string
  kpp?: string
  address: string
  email: string
  phone: string
  domain: string
}

/** Международная ветка: /en, англоязычные юрдоки. */
export const COMPANY: Company = {
  name: 'Celementa FZCO',
  regNo: '16116518',
  address: 'Dubai Silicon Oasis, DDP, Building A2, 341041, Dubai, UAE',
  email: 'support@mezmo-song.com',
  phone: '+7 911 816-89-33',
  domain: 'mezmo-song.com',
}

/** Российская ветка: /ru, русскоязычные юрдоки. */
export const RU_COMPANY: Company = {
  name: 'ООО «МБК»',
  inn: '7708364569',
  ogrn: '1197746677402',
  kpp: '770801001',
  address: '107140, г. Москва, вн.тер.г. Муниципальный Округ Красносельский, пер. 1-й Красносельский, д. 3, подв. 1, пом/ком/оф. I/17/B-03',
  email: 'support@mezmo-song.com',
  phone: '+7 911 816-89-33',
  domain: 'mezmo-song.com',
}

/** Реквизиты страницы: на /ru — МБК, на /en — FZCO. */
export function companyFor(lang: 'ru' | 'en'): Company {
  return lang === 'en' ? COMPANY : RU_COMPANY
}

/**
 * Строки реквизитов для футера: название, номера, адрес. Одна реализация на
 * футер лендинга, контакты и юридические страницы — иначе они разъедутся.
 */
export function companyLines(company: Company, lang: 'ru' | 'en'): string[] {
  const lines = [company.name]
  if (company.regNo) lines.push(lang === 'en' ? `Reg. No. ${company.regNo}` : `Рег. № ${company.regNo}`)
  if (company.inn) {
    lines.push(
      lang === 'en'
        ? `INN ${company.inn} · OGRN ${company.ogrn} · KPP ${company.kpp}`
        : `ИНН ${company.inn} · ОГРН ${company.ogrn} · КПП ${company.kpp}`,
    )
  }
  lines.push(company.address)
  return lines
}

export type LegalDoc = 'terms' | 'privacy' | 'security' | 'consent'

// Юридические страницы (тексты — src/legal/*.md). Порядок — как в футере.
export const LEGAL_LINKS: { doc: LegalDoc; label: { ru: string; en: string } }[] = [
  { doc: 'terms', label: { ru: 'Пользовательское соглашение', en: 'Terms of Service' } },
  { doc: 'privacy', label: { ru: 'Политика конфиденциальности', en: 'Privacy Policy' } },
  { doc: 'security', label: { ru: 'Политика информационной безопасности', en: 'Information Security Policy' } },
  { doc: 'consent', label: { ru: 'Согласие на обработку персональных данных', en: 'Consent to the Processing of Personal Data' } },
]

/**
 * Юрлицо документов: 'ru' — ООО «МБК», 'en' — Celementa FZCO.
 *
 * Ветка важнее языка страницы: на /en можно выбрать российскую карту, и тогда
 * человеку показываются документы МБК, а на /ru с международной картой —
 * документы FZCO. На лендинге ветки ещё нет, и юрлицо берётся по языку.
 */
export type LegalEntity = 'ru' | 'en'

export function legalEntityForBranch(branch?: 'ru' | 'intl' | null): LegalEntity {
  return branch === 'ru' ? 'ru' : 'en'
}

/** Запасной вариант, когда ветка неизвестна: ЮKassa — рубли, остальные — FZCO. */
export function legalEntityForProvider(provider?: string | null): LegalEntity {
  return provider === 'yookassa' ? 'ru' : 'en'
}

/**
 * Ссылка на документ выбранного юрлица. Язык страницы остаётся текущим
 * (localePath), а `?entity=` говорит LegalPage, какие тексты и реквизиты
 * показывать: язык интерфейса и юрлицо — разные вещи.
 */
export function legalDocPath(doc: LegalDoc, entity: LegalEntity, lang?: 'ru' | 'en'): string {
  return `${localePath(`/${doc}`, lang)}?entity=${entity}`
}
