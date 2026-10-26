// Реквизиты юрлица mezmo-song.com — единственное место, откуда их берут футер,
// контакты и юридические страницы.
export const COMPANY = {
  name: 'Celementa FZCO',
  regNo: '16116518',
  address: 'Dubai Silicon Oasis, DDP, Building A2, 341041, Dubai, UAE',
  email: 'support@mezmo-song.com',
  phone: '+7 911 816-89-33',
  domain: 'mezmo-song.com',
}

export type LegalDoc = 'terms' | 'privacy' | 'security' | 'consent'

// Юридические страницы (тексты — src/legal/*.md). Порядок — как в футере.
export const LEGAL_LINKS: { doc: LegalDoc; label: { ru: string; en: string } }[] = [
  { doc: 'terms', label: { ru: 'Пользовательское соглашение', en: 'Terms of Service' } },
  { doc: 'privacy', label: { ru: 'Политика конфиденциальности', en: 'Privacy Policy' } },
  { doc: 'security', label: { ru: 'Политика информационной безопасности', en: 'Information Security Policy' } },
  { doc: 'consent', label: { ru: 'Согласие на обработку персональных данных', en: 'Consent to the Processing of Personal Data' } },
]
