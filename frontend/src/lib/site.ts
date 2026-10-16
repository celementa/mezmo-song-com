// Флаг сборки intl-посадочной (mezmo-song.com): `npm run build:intl`.
// В обычном бандле константа false и intl-ветки вырезаются на этапе сборки.
export const IS_INTL = import.meta.env.VITE_SITE === 'intl'

// Реквизиты юрлица intl-сайта. Пустые поля не рендерятся — заполнить, когда дадут.
export const INTL_COMPANY = {
  name: 'Celementa FZCO',
  country: 'UAE',
  regNo: '16116518',
  address: 'Dubai Silicon Oasis, DDP, Building A2, 341041, Dubai, UAE',
  email: 'support@mezmo-song.com',
  phone: '+7 911 816-89-33',
  domain: 'mezmo-song.com',
}
