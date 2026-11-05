// Реквизиты юрлиц mezmo-song.com — единственное место, откуда их берут футер,
// контакты и юридические страницы.
//
// Юрлицо зависит от языка страницы, потому что от него зависит платёжная ветка:
// рублёвые платежи идут на российское ООО «МБК» (ЮKassa), международные — на
// Celementa FZCO (Stripe). Русские юрдоки (src/legal/*.md) описывают МБК,
// английские (src/legal/en/*.md) — FZCO.
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
  ogrn: '1097746427536',
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
