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
export const LEGAL_LINKS: { doc: LegalDoc; label: string }[] = [
  { doc: 'terms', label: 'Пользовательское соглашение' },
  { doc: 'privacy', label: 'Политика конфиденциальности' },
  { doc: 'security', label: 'Политика информационной безопасности' },
  { doc: 'consent', label: 'Согласие на обработку персональных данных' },
]
