import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { apiGet, apiPost } from '@/lib/http'
import { getAttribution } from '@/lib/attribution'
import { isLocalhost } from '@/lib/isLocalhost'
import { getSessionId } from '@/lib/session'
import type { Loose } from '@/lib/loose'
import { cachedSettingsGet } from '@/lib/settingsCache'
import { useThemeSlug } from '@/features/theme/ThemeContext'
import type { ActivityToastConfig } from '@/components/activity-toast/ActivityToast'
import type { TrackItem } from '@/components/landing/AudioSamplesSection'
import { resolveSectionOrder, resolveSectionVisibility } from '@/lib/landingSections'
import { fmtCurrency, ruDaysPhrase, ruHoursPhrase } from '@/routes/config/utils'
import { inCurrency, useCurrency, type Currency } from '@/lib/currency'
import { useLang, useTr, type Lang } from '@/lib/lang'
import enLandingOverlay from '../../intl/landing.en.json'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type PublicSiteConfig = {
  landing?: {
    heroHeadline?: { line1?: string; line2?: string }
    heroSubtext?: string
    footerCta?: { headline?: string; subtitle?: string; securityBadge?: string; quotaLine?: string }
    heroMedia?: { imageUrl?: string; videoUrl?: string | null; mode?: string; videoBelowPrice?: boolean; videoThumbnailUrl?: string | null }
    heroOverlay?: {
      quote?: string
      authorName?: string
      authorLocation?: string
      authorAvatarUrl?: string | null
    }
    heroPlayer?: {
      enabled?: boolean
      playingBadgeText?: string
      cornerBadgeText?: string
      verifiedBadgeText?: string
      quote?: string
      authorName?: string
      authorSubline?: string
      authorAvatarUrl?: string | null
      audioUrl?: string | null
      stopAfterOnePlay?: boolean
      stopCtaButtonText?: string
      useVideoAudio?: boolean
      videoPreviewLoop?: boolean
      videoPreviewLoopSeconds?: number
    }
    audioSamples?: {
      nowPlaying?: { name?: string; quote?: string; time?: string; metaText?: string | null; audioUrl?: string | null; thumbnailUrl?: string | null }
      playlist?: Array<{ title?: string; subtitle?: string; ctaLabel?: string; audioUrl?: string | null; thumbnailUrl?: string | null }>
    }
  }
  heroCheckmarks?: string[]
  trustBadges?: { badge1?: string; badge2?: string; badge3?: string }
  statsBar?: { items?: Array<{ val?: string; label?: string }> }
  promoBanner?: {
    enabled?: boolean
    countdownLabel?: string
    countdownTargetDate?: string
    promoBadgeText?: string
    quotaBadgeText?: string
    evergreenEnabled?: boolean
    evergreenCycleHours?: number
  }
  reviews?: {
    sectionLabel?: string
    sectionHeadline?: string
    sectionSubtext?: string
    items?: Array<{
      style?: 'accent' | 'dark-chat' | 'white' | 'video' | 'image'
      quote?: string
      chatMessages?: string[]
      authorName?: string
      authorMeta?: string
      authorAvatarUrl?: string
      videoUrl?: string
      videoThumbnailUrl?: string
      imageUrl?: string
    }>
  }
  activityToast?: ActivityToastConfig
  colors?: { accentColor?: string; buttonColor?: string; bgColor1?: string; bgColor2?: string }
  audioSamplesSection?: {
    badge?: string
    headline?: string
    subtext?: string
    otherLabel?: string
    ctaLine?: string
    ctaButtonText?: string
    backgroundColor?: string
  }
  comparisonSection?: {
    headline?: string
    giftItems?: Array<{ icon?: string; name?: string; price?: string }>
    forgottenLabel?: string
    foreverLabel?: string
    bestPriceBadge?: string
    productTitle?: string
    checklistItems?: Array<{ text?: string }>
  }
  howItWorksSection?: {
    hidden?: boolean
    label?: string
    headline?: string
    steps?: Array<{ icon?: string; title?: string; desc?: string }>
    backgroundType?: 'color' | 'image'
    backgroundColor?: string
    backgroundImageUrl?: string
    backgroundOverlayOpacity?: number
    ctaButtonText?: string
    ctaButtonColor?: string
  }
  guaranteeSection?: {
    badge?: string
    headline?: string
    description?: string
    badges?: string[]
    videoUrl?: string
  }
  faqSection?: {
    hidden?: boolean
    headline?: string
    items?: Array<{ q?: string; a?: string }>
  }
  footer?: {
    tagline?: string
    companyName?: string
    email?: string
    phone?: string
    disclaimer?: string
    copyrightLine?: string
  }
  miscText?: {
    heroStarLine?: string
    heroTagline?: string
    headlineFont?: string
    ctaButtonText?: string
    heroCtaButtonText?: string
    heroChipsHeadline?: string
    mobileCtaQuotaBadge?: string
  }
  priceVisibility?: {
    promoBanner?: boolean
    header?: boolean
    heroCtaButton?: boolean
    audioSamplesButton?: boolean
    comparisonSection?: boolean
    howItWorksButton?: boolean
    footerCtaButton?: boolean
    mobileStickyButton?: boolean
    funnelHeader?: boolean
    orderSummary?: boolean
    checkoutButton?: boolean
  }
  configSteps?: Loose
  sectionVisibility?: Loose
  sectionOrder?: Loose
  logoUrl?: string
  mobileLogoUrl?: string
  faviconUrl?: string
}

type PublicSettingsResponse = {
  publicSiteConfig: PublicSiteConfig | null
  instantEnabled?: boolean
  deliveryDelayHours?: number
  paymentAmount?: number
  originalAmount?: number
  design?: string
}

export type FaqItem = { q: string; a: string }

export type ShowPrice = {
  promoBanner: boolean
  header: boolean
  heroCtaButton: boolean
  audioSamplesButton: boolean
  comparisonSection: boolean
  howItWorksButton: boolean
  footerCtaButton: boolean
  mobileStickyButton: boolean
}

export type DeliveryEta = {
  label: string
  sentenceLower: string
  short: string
}

export type PromoBannerData = {
  enabled: boolean
  countdownLabel: string
  countdownTargetDate: string
  promoBadgeText: string
  quotaBadgeText: string
  evergreenEnabled: boolean
  evergreenCycleHours: number
}

export type HeroData = {
  headlineLine1: string
  headlineLine2: string
  subtext: string
  imageUrl: string
  videoUrl: string | null
  mediaMode: 'image' | 'video'
  videoBelowPrice: boolean
  videoThumbnailUrl: string | null
  headlineFont: string
  overlay: {
    quote: string
    authorName: string
    authorLocation: string
    authorAvatarUrl: string | null
  }
  player: {
    enabled: boolean
    audioUrl: string | null
    playingBadgeText: string
    cornerBadgeText: string
    verifiedBadgeText: string
    quote: string
    authorName: string
    authorSubline: string
    authorAvatarUrl: string | null
    stopAfterOnePlay: boolean
    useVideoAudio: boolean
    stopCtaButtonText: string
  }
  chips: Array<{ label: string; icon: string; value: string }>
  trustBadges: { badge1: string; badge2: string; badge3: string }
}

export type SectionsData = {
  audioSamples: PublicSiteConfig['audioSamplesSection']
  comparison: PublicSiteConfig['comparisonSection']
  howItWorks: PublicSiteConfig['howItWorksSection']
  guarantee: PublicSiteConfig['guaranteeSection']
  faq: PublicSiteConfig['faqSection']
  footer: PublicSiteConfig['footer']
  footerCta: {
    headline: string
    subtitle: string
    securityBadge?: string
    quotaLine?: string
  }
  reviews: {
    sectionLabel: string
    sectionHeadline: string
    sectionSubtext: string
    items: NonNullable<NonNullable<PublicSiteConfig['reviews']>['items']>
  }
  statsBar: { items: Array<{ val: string; label: string }> }
  faqItems: FaqItem[]
}

/** Everything the landing page needs to render. */
export type LandingData = {
  themeSlug: string | null
  /** Template design ID (e.g. "classic", "minimal"). null while loading. */
  design: string | null
  themeStyle: React.CSSProperties
  logoUrl: string
  mobileLogoUrl: string

  // Pricing & delivery
  fmtCurrency: (amt: number | null | undefined) => string
  paymentAmount: number | null
  originalAmount: number | null
  deliveryEta: DeliveryEta
  showPrice: ShowPrice

  // Promo
  promoBanner: PromoBannerData

  // Hero
  hero: HeroData

  // Misc text
  miscText: PublicSiteConfig['miscText']

  // Section ordering & visibility
  sectionOrder: string[]
  sectionVisibility: Record<string, boolean>

  // Section data
  sections: SectionsData

  // Track data for audio player
  allTracks: TrackItem[]

  // Activity toast
  toastConfig: ActivityToastConfig

  // Interactive state
  heroOpen: boolean
  setHeroOpen: (v: boolean) => void
  heroVideoPlaying: boolean
  setHeroVideoPlaying: (v: boolean) => void
  selectedTrackIndex: number
  setSelectedTrackIndex: (n: number) => void
  autoPlayOnSelect: boolean
  setAutoPlayOnSelect: (b: boolean) => void
  showMobileCta: boolean
  videoReady: boolean

  // Refs
  heroRef: React.RefObject<HTMLElement | null>
  heroVideoRef: React.RefObject<HTMLVideoElement | null>

  // Utilities
  sanitizeHtml: (html: string) => string
  resolveAsset: (v: string) => string
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

const defaultPublicSiteConfig: PublicSiteConfig = {
  landing: {
    heroHeadline: { line1: 'В этот особенный день', line2: 'троните близкого до слёз.' },
    heroSubtext: '',
    footerCta: {
      headline: 'Не упусти момент, сделай заказ по спеццене',
      subtitle:
        'Подарите то, что останется в памяти навсегда. Уже <strong>тысячи</strong> людей увидели искреннюю благодарность в глазах тех, кому сделали подарок.',
    },
    heroMedia: {
      imageUrl: '/images/hero-poster.jpg',
      videoUrl: '/hero.mp4',
    },
    heroOverlay: {
      quote: 'Не дослушал до конца припева…',
      authorName: 'Марина',
      authorLocation: 'Москва',
      authorAvatarUrl: 'https://images.unsplash.com/photo-1530254541043-129f4c372200?w=200&h=200&fit=crop',
    },
    heroPlayer: {
      enabled: true,
      playingBadgeText: 'Сейчас',
      cornerBadgeText: 'Спецпредложение',
      verifiedBadgeText: 'Проверенный заказ',
      quote: 'Сдерживался — не получилось.',
      authorName: 'Александр',
      authorSubline: 'СПб \u2022 2026',
      authorAvatarUrl: 'https://images.unsplash.com/photo-1530254541043-129f4c372200?w=200&h=200&fit=crop',
      audioUrl: null,
    },
    audioSamples: {
      nowPlaying: {
        name: 'Для Сергея — навсегда',
        quote: 'Каждый наш вечер, с того неловкого первого свидания…',
        time: '3:24',
        metaText: 'Москва \u2022 Проверено',
        audioUrl: null,
      },
      playlist: [
        { title: 'Для Анны — всё для тебя', subtitle: 'Кантри \u2022 3:12', ctaLabel: 'СЛУШАТЬ' },
        { title: 'Для Максима — 10 лет вместе', subtitle: 'Акустика \u2022 2:58', ctaLabel: 'СЛУШАТЬ' },
        { title: 'Для Оли — это наше приключение', subtitle: 'Поп-баллада \u2022 3:45', ctaLabel: 'СЛУШАТЬ' },
      ],
    },
  },
  activityToast: {
    enabled: true,
    intervalMs: 10000,
    durationMs: 4500,
    items: [
      { fullName: 'Игорь Николаев', city: 'Казань', recipientName: 'Алина' },
      { fullName: 'Елена Орлова', city: 'Москва', recipientName: 'Павел' },
      { fullName: 'Даниил Волков', city: 'Сочи', recipientName: 'Марина' },
    ],
  },
}

const defaultPublicSiteConfigGold: PublicSiteConfig & { reviews?: { items?: Array<{ style?: string; imageUrl?: string }> } } = {
  landing: {
    heroHeadline: { line1: 'Она включила запись —', line2: 'и растрогалась на первой строчке' },
    heroSubtext: 'Персональная песня с историей и именем близкого. Текст и музыку создаёт ИИ, доставляем сразу — не через сутки.',
    footerCta: {
      headline: 'Подарите то, что запомнят навсегда',
      subtitle:
        'Тысячи людей уже увидели искреннюю благодарность в глазах тех, кому сделали такой подарок. Ваша очередь.',
    },
    heroMedia: {
      imageUrl: '/images/hero-poster.jpg',
      videoUrl: '/hero.mp4',
    },
    heroOverlay: {
      quote: 'Не дослушал до конца припева…',
      authorName: 'Марина',
      authorLocation: 'Москва',
      authorAvatarUrl: 'https://images.unsplash.com/photo-1530254541043-129f4c372200?w=200&h=200&fit=crop',
    },
    heroPlayer: {
      enabled: true,
      playingBadgeText: 'Сейчас',
      cornerBadgeText: 'Спецпредложение',
      verifiedBadgeText: 'Проверенный заказ',
      quote: 'Сдерживался — не получилось.',
      authorName: 'Александр',
      authorSubline: 'СПб • 2026',
      authorAvatarUrl: 'https://images.unsplash.com/photo-1530254541043-129f4c372200?w=200&h=200&fit=crop',
      audioUrl: null,
    },
    audioSamples: {
      // Реальные примеры песен (скачаны с прода в статику /audio —
      // прод-прокси заказов отдаёт файл ~15 сек, статика играет мгновенно).
      nowPlaying: {
        name: 'Для Анны — моя любовь',
        quote: 'Каждый наш вечер, с того неловкого первого свидания…',
        time: '3:23',
        metaText: 'Поп-баллада',
        audioUrl: '/audio/sample-anna.mp3',
      },
      playlist: [
        { title: 'Для Максима — 10 лет вместе', subtitle: 'Акустика • 3:20', ctaLabel: 'СЛУШАТЬ', audioUrl: '/audio/sample-maxim.mp3' },
        { title: 'Для Оли — это наша история', subtitle: 'Поп • 3:29', ctaLabel: 'СЛУШАТЬ', audioUrl: '/audio/sample-olya.mp3' },
      ],
    },
  },
  statsBar: {
    items: [
      { val: '96%', label: 'получателей растрогались' },
      { val: '12 400+', label: 'песен создано' },
      { val: '10–30 мин', label: 'до готовой песни' },
      { val: '4.9 ★', label: 'средняя оценка' },
    ],
  },
  trustBadges: {
    badge1: '★★★★★ 12 400+ песен',
    badge2: '10–30 минут',
    badge3: 'Возврат 100%',
  },
  faqSection: {
    headline: 'Частые вопросы',
    items: [
      {
        q: 'Используете ли вы настоящих авторов?',
        a: 'Да, каждую песню пишет живой человек — профессиональный автор. ИИ помогает с мелодией и аранжировкой.',
      },
      {
        q: 'Сколько ждать готовую песню?',
        a: 'Обычно 10–30 минут. Срок может быть дольше в часы пиков.',
      },
      {
        q: 'Есть ли гарантия?',
        a: 'Да, возврат 100%, если не понравится. Дорабатываем бесплатно, пока не станет ок.',
      },
      {
        q: 'Какие жанры доступны?',
        a: 'Поп-баллада, акустика, рок и другие. Выбираете при оформлении заказа.',
      },
      {
        q: 'Это не просто ИИ без участия человека?',
        a: 'Живой автор пишет слова под вашу историю, ИИ помогает с музыкой. Не шаблон, индивидуальная песня.',
      },
      {
        q: 'Можно подарить на любой праздник?',
        a: 'Да, подходит для дня рождения, годовщины, предложения, выпускного, просто так. Любой повод — главное история.',
      },
    ],
  },
  reviews: {
    sectionLabel: 'Реакции получателей',
    sectionHeadline: 'Говорят наши клиенты',
    sectionSubtext: '2 847+ довольных клиентов',
    items: [
      {
        style: 'image',
        imageUrl: '/images/review-telegram-galya.png',
        authorName: 'Галя • Telegram',
      },
      {
        style: 'image',
        imageUrl: '/images/review-whatsapp-proposal.png',
        authorName: 'Дмитрий Р. • WhatsApp',
      },
      {
        style: 'image',
        imageUrl: '/images/review-max-olga.png',
        authorName: 'Ольга В. • MAX',
      },
    ],
  },
  activityToast: {
    enabled: true,
    intervalMs: 10000,
    durationMs: 4500,
    items: [
      { fullName: 'Мария из Казани', city: 'Казань', recipientName: 'для мамы' },
      { fullName: 'Дмитрий Р.', city: 'Москва', recipientName: 'предложение' },
      { fullName: 'Мария К.', city: 'СПб', recipientName: 'для бабушки' },
    ],
  },
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

function sanitizeHtml(html: string): string {
  return html.replace(/<\/?(?!strong|em|br\s*\/?)([a-z][a-z0-9]*)\b[^>]*>/gi, '')
}

/** Английский оверлей текстов поверх publicSiteConfig: объекты сливаются глубоко, массивы заменяются целиком. */
function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

function deepMerge(base: unknown, over: unknown): unknown {
  if (isPlainObject(base) && isPlainObject(over)) {
    const out: Record<string, unknown> = { ...base }
    for (const k of Object.keys(over)) out[k] = deepMerge(base[k], over[k])
    return out
  }
  return over
}

function withLangOverlay<T>(cfg: T, lang: Lang): T {
  if (lang !== 'en' || !isPlainObject(cfg)) return cfg
  return deepMerge(cfg, enLandingOverlay) as T
}

function enPhrase(n: number, unit: 'day' | 'hour') {
  return `${n} ${unit}${n === 1 ? '' : 's'}`
}

function fmtCurrencyGlobal(amt: number | null | undefined, currency: Currency, lang: Lang) {
  if (amt === 0) return lang === 'en' ? 'Free' : 'Бесплатно'
  if (amt === null || amt === undefined) return fmtCurrency(25, currency)
  return fmtCurrency(amt, currency)
}

function getInlinedConfig(): { cfg: PublicSiteConfig | null | undefined; design: string | null; instantEnabled: boolean | null; deliveryDelayHours: number | null; paymentAmount: number | null; originalAmount: number | null } {
  const inlined = (window as Loose).__SETTINGS__
  if (inlined && typeof inlined === 'object' && inlined.publicSiteConfig) {
    return {
      cfg: inlined.publicSiteConfig as PublicSiteConfig,
      design: typeof inlined.design === 'string' && inlined.design ? inlined.design : null,
      instantEnabled: typeof inlined.instantEnabled === 'boolean' ? inlined.instantEnabled : null,
      deliveryDelayHours: typeof inlined.deliveryDelayHours === 'number' ? inlined.deliveryDelayHours : null,
      paymentAmount: typeof inlined.paymentAmount === 'number' ? inlined.paymentAmount : null,
      originalAmount: typeof inlined.originalAmount === 'number' ? inlined.originalAmount : null,
    }
  }
  return { cfg: undefined, design: null, instantEnabled: null, deliveryDelayHours: null, paymentAmount: null, originalAmount: null }
}


// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export function useLandingData(): LandingData {
  const themeSlug = useThemeSlug()
  const lang = useLang()
  const tr = useTr()
  const defaultReviewItems = [
    {
      style: 'white' as const,
      quote: tr('Муж услышал первые строчки и сразу понял, что это про нас. Отложил телефон и просто обнял меня.', 'My husband heard the first lines and knew right away it was about us. He put his phone down and just hugged me.'),
      authorName: tr('Мария К.', 'Mary K.'),
      authorMeta: tr('Подтверждённый покупатель', 'Verified customer'),
    },
    {
      style: 'white' as const,
      quote: tr('Мама плакала 10 минут. Сказала, что это самый красивый подарок, который ей когда-либо делали.', 'Mom cried for 10 minutes. She said it was the most beautiful gift anyone had ever given her.'),
      authorName: tr('Елена М.', 'Helen M.'),
      authorMeta: tr('Подтверждённый покупатель', 'Verified customer'),
    },
    {
      style: 'white' as const,
      quote: tr('Сделал предложение, пока играла песня. Она сказала «да» ещё до финального куплета. Просто волшебство.', 'I proposed while the song was playing. She said yes before the final verse. Pure magic.'),
      authorName: tr('Дмитрий Р.', 'Dan R.'),
      authorMeta: tr('Подтверждённый покупатель', 'Verified customer'),
    },
  ]
  const _inlined = useMemo(() => getInlinedConfig(), [])
  const [rawPublicSiteConfig, setPublicSiteConfig] = useState<PublicSiteConfig | null | undefined>(_inlined.cfg)
  // Язык меняется навигацией без перезагрузки: оверлей пересчитывается от текущего языка.
  const publicSiteConfig = useMemo(() => withLangOverlay(rawPublicSiteConfig, lang), [rawPublicSiteConfig, lang])
  // Инициализируемся из инлайн-настроек (window.__SETTINGS__), чтобы реальный
  // шаблон отрисовался сразу на маунте, не дожидаясь /api/public/settings (LCP).
  const [design, setDesign] = useState<string | null>(_inlined.design)
  const [heroOpen, setHeroOpen] = useState(false)
  const [heroVideoPlaying, setHeroVideoPlaying] = useState(false)
  const [selectedTrackIndex, setSelectedTrackIndex] = useState<number>(0)
  const [autoPlayOnSelect, setAutoPlayOnSelect] = useState(false)
  const [showMobileCta, setShowMobileCta] = useState(false)
  const heroRef = useRef<HTMLElement>(null)
  const heroVideoRef = useRef<HTMLVideoElement>(null)

  // Грузим src видео только после события `load` (+небольшая пауза), когда poster
  // уже отрисован как LCP, а критические ресурсы догружены. Так загрузка/декод
  // hero.mp4 не конкурирует за поток и сеть в окне LCP и не раздувает TBT.
  const [videoReady, setVideoReady] = useState(false)
  useEffect(() => {
    let timer: number | undefined
    const arm = () => {
      timer = window.setTimeout(() => setVideoReady(true), 600)
    }
    if (document.readyState === 'complete') {
      arm()
      return () => {
        if (timer) clearTimeout(timer)
      }
    }
    window.addEventListener('load', arm, { once: true })
    return () => {
      window.removeEventListener('load', arm)
      if (timer) clearTimeout(timer)
    }
  }, [])

  const [instantEnabled, setInstantEnabled] = useState<boolean | null>(_inlined.instantEnabled)
  const [deliveryDelayHours, setDeliveryDelayHours] = useState<number | null>(_inlined.deliveryDelayHours)
  const [paymentAmount, setPaymentAmount] = useState<number | null>(_inlined.paymentAmount)
  const [originalAmount, setOriginalAmount] = useState<number | null>(_inlined.originalAmount)

  // Своя функция на каждую валюту: компоненты, получившие её пропсом, видят
  // новую ссылку и перерисовывают цены при переключении «$ / ₽».
  const currency = useCurrency()
  const fmtCurrency = useCallback((amt: number | null | undefined) => fmtCurrencyGlobal(amt, currency, lang), [currency, lang])

  // --- Tracking ---
  useEffect(() => {
    // Визит нужен дашборду: конверсия плеча считается от визита. Localhost не
    // трекается, кроме Vite-дев-стенда (import.meta.env.DEV) с локальной БД.
    if (isLocalhost() && !import.meta.env.DEV) return
    try {
      apiPost('/api/public/track', {
        path: themeSlug ? `/${themeSlug}` : '/',
        sessionId: getSessionId(),
        themeSlug: themeSlug ?? null,
        attribution: getAttribution(),
      }).catch(() => {})
    } catch {
      // визит — телеметрия: её сбой не должен ломать рендер лендинга
    }
  }, [])

  // --- Delivery ETA ---
  const deliveryEta = useMemo<DeliveryEta>(() => {
    if (instantEnabled) {
      return lang === 'en'
        ? { label: '2-5 minutes', sentenceLower: 'in 2-5 minutes', short: '2-5 min' }
        : { label: '2-5 минут', sentenceLower: 'за 2-5 минут', short: '2-5 мин' }
    }
    const hRaw = deliveryDelayHours
    const h = typeof hRaw === 'number' && Number.isFinite(hRaw) && hRaw > 0 ? hRaw : 24
    if (h >= 24 && h % 24 === 0) {
      const d = h / 24
      const phrase = lang === 'en' ? enPhrase(d, 'day') : ruDaysPhrase(d)
      return { label: tr(`За ${phrase}`, `In ${phrase}`), sentenceLower: tr(`за ${phrase}`, `in ${phrase}`), short: phrase }
    }
    const phrase = lang === 'en' ? enPhrase(h, 'hour') : ruHoursPhrase(h)
    return { label: tr(`За ${phrase}`, `In ${phrase}`), sentenceLower: tr(`за ${phrase}`, `in ${phrase}`), short: phrase }
  }, [deliveryDelayHours, instantEnabled, lang, tr])

  // --- FAQ items ---
  const faqItems = useMemo<FaqItem[]>(() => {
    const cfgFaq = (publicSiteConfig as Loose)?.faqSection
    if (cfgFaq?.items && Array.isArray(cfgFaq.items) && cfgFaq.items.length > 0) {
      return cfgFaq.items
        .filter((it: Loose) => it?.q && it?.a)
        .map((it: Loose) => ({ q: String(it.q), a: String(it.a) }))
    }
    const etaAnswerStart = instantEnabled ? tr('2-5 минут', '2-5 minutes') : deliveryEta.label
    const etaAnswer = tr(
      `Обычно ${etaAnswerStart.toLowerCase()}. Пришлём уведомление на электронную почту, когда будет готово.`,
      `Usually ${etaAnswerStart.toLowerCase()}. We'll send you an email as soon as it's ready.`,
    )
    return [
      {
        q: tr('Он/она «совсем не сентиментальный»…', 'He/she is "not sentimental at all"…'),
        a: tr(
          'Так отвечают почти все 😊 На деле очень многих тронуло до слёз — чем суше фасад, тем сильнее эффект. \ud83d\ude09',
          'That\'s what almost everyone says 😊 In reality, so many people are moved to tears — the drier the exterior, the bigger the effect. \ud83d\ude09',
        ),
      },
      {
        q: tr('Как человек получит песню?', 'How will they receive the song?'),
        a: tr(
          'Ссылка для скачивания придёт на электронную почту. Можете включить вместе, переслать в мессенджер или преподнести сюрпризом. Это обычный MP3.',
          'The download link arrives by email. You can play it together, forward it to a messenger, or give it as a surprise. It\'s a regular MP3.',
        ),
      },
      { q: tr('Как долго ждать?', 'How long do I wait?'), a: etaAnswer },
      {
        q: tr('А если результат не понравится?', 'What if I don\'t like the result?'),
        a: tr(
          'Дорабатываем бесплатно, пока не станет ок. Если совсем не зайдёт — полный возврат без лишних вопросов. \ud83d\udc95',
          'We revise it for free until it\'s right. If it just doesn\'t work, you get a full refund, no questions asked. \ud83d\udc95',
        ),
      },
      {
        q: tr('Правда бесплатно / со скидкой?', 'Is it really free / discounted?'),
        a: tr(
          `Для первых клиентов — спецусловие (полная цена обычно ${fmtCurrency(originalAmount)}). Скрытых платежей нет.`,
          `For first-time customers — a special offer (the regular price is usually ${fmtCurrency(originalAmount)}). No hidden fees.`,
        ),
      },
    ]
  }, [deliveryEta.label, instantEnabled, publicSiteConfig, originalAmount, fmtCurrency, tr])

  // --- Scroll listener for mobile CTA ---
  useEffect(() => {
    const handleScroll = () => { setShowMobileCta(window.scrollY > 400) }
    window.addEventListener('scroll', handleScroll, { passive: true })
    handleScroll()
    return () => window.removeEventListener('scroll', handleScroll)
  }, [])

  // --- Structured data (JSON-LD) ---
  useEffect(() => {
    const origin = typeof window !== 'undefined' && window.location?.origin ? window.location.origin : 'https://example.com'
    const organization = {
      '@context': 'https://schema.org',
      '@type': 'Organization',
      '@id': `${origin}/#organization`,
      name: tr('Персональная песня', 'Personalized song'),
      url: origin,
      description: tr(
        'Персональная песня с именем близкого в тексте. Профессиональная аранжировка.',
        'A personalized song with your loved one\'s name in the lyrics. Professional arrangement.',
      ),
      foundingDate: '2024',
      sameAs: [],
    }
    const service = {
      '@context': 'https://schema.org',
      '@type': 'Service',
      name: tr('Персональная песня на заказ', 'Custom personalized song'),
      description: tr(
        `Песня с вашей историей в тексте и студийным звуком. Срок ${deliveryEta.sentenceLower}.`,
        `A song with your story in the lyrics and studio-quality sound. Delivered ${deliveryEta.sentenceLower}.`,
      ),
      provider: { '@id': `${origin}/#organization` },
      offers: {
        '@type': 'Offer',
        price: String(inCurrency(paymentAmount ?? 25, currency)),
        priceCurrency: currency,
        availability: 'https://schema.org/InStock',
      },
    }
    const scriptOrg = document.createElement('script')
    scriptOrg.type = 'application/ld+json'
    scriptOrg.textContent = JSON.stringify(organization)
    scriptOrg.id = 'ld-organization'
    const scriptSvc = document.createElement('script')
    scriptSvc.type = 'application/ld+json'
    scriptSvc.textContent = JSON.stringify(service)
    scriptSvc.id = 'ld-service'
    document.head.appendChild(scriptOrg)
    document.head.appendChild(scriptSvc)
    const faqHidden = (publicSiteConfig as Loose)?.faqSection?.hidden === true
    if (!faqHidden) {
      const faqPage = {
        '@context': 'https://schema.org',
        '@type': 'FAQPage',
        mainEntity: faqItems.map(({ q, a }) => ({
          '@type': 'Question',
          name: q,
          acceptedAnswer: { '@type': 'Answer', text: a.replace(/\ud83d\ude09|\ud83d\udc95/g, '') },
        })),
      }
      const scriptFaq = document.createElement('script')
      scriptFaq.type = 'application/ld+json'
      scriptFaq.textContent = JSON.stringify(faqPage)
      scriptFaq.id = 'ld-faq'
      document.head.appendChild(scriptFaq)
    }
    return () => {
      document.getElementById('ld-organization')?.remove()
      document.getElementById('ld-service')?.remove()
      document.getElementById('ld-faq')?.remove()
    }
  }, [deliveryEta.sentenceLower, faqItems, publicSiteConfig, paymentAmount, currency, tr])

  // --- Fetch settings ---
  useEffect(() => {
    let cancelled = false
    const params = new URLSearchParams()
    if (themeSlug) params.set('theme', themeSlug)
    // Плечо из рекламной ссылки (/?arm=variant1-500): сервер отдаст цену этого
    // плеча, и главная покажет её вместо базовой. Инлайн-снимок настроек мимо —
    // ключ запроса с параметром его не совпадает.
    const armName = new URLSearchParams(window.location.search).get('arm')
    if (armName) params.set('arm', armName)
    const qs = params.toString()
    cachedSettingsGet<PublicSettingsResponse>(`/api/public/settings${qs ? `?${qs}` : ''}`)
      .then((res) => {
        if (cancelled) return
        const cfg = res?.publicSiteConfig
        if (cfg && typeof cfg === 'object') {
          setPublicSiteConfig(cfg)
          const heroImg = (cfg as Loose)?.landing?.heroMedia?.imageUrl
          if (typeof heroImg === 'string' && heroImg.trim() && !document.getElementById('preload-hero')) {
            const base = import.meta.env.VITE_API_BASE_URL ?? ''
            const link = document.createElement('link')
            link.id = 'preload-hero'
            link.rel = 'preload'
            link.as = 'image'
            link.setAttribute('fetchpriority', 'high')
            link.href = /^https?:\/\//i.test(heroImg) ? heroImg : (base + heroImg)
            document.head.appendChild(link)
          }
        }
        else setPublicSiteConfig(null)
        setInstantEnabled(typeof res?.instantEnabled === 'boolean' ? res.instantEnabled : null)
        setDeliveryDelayHours(typeof res?.deliveryDelayHours === 'number' ? res.deliveryDelayHours : null)
        setPaymentAmount(typeof res?.paymentAmount === 'number' ? res.paymentAmount : null)
        setOriginalAmount(typeof res?.originalAmount === 'number' ? res.originalAmount : null)
        setDesign(typeof res?.design === 'string' && res.design ? res.design : 'classic')
      })
      .catch(() => {
        if (!cancelled) { setPublicSiteConfig(null); setDesign('classic') }
      })
    // Тексты и дизайн лендинга — из вшитого снимка, а цена плеча из ссылки —
    // с сервера: та же цена, что потом выставит воронка (/?arm=variant3-500-750s → $19).
    if (armName) {
      apiGet<{ price?: number }>(`/api/public/flow?sid=${encodeURIComponent(getSessionId())}&arm=${encodeURIComponent(armName)}`)
        .then((cfg) => {
          if (!cancelled && typeof cfg?.price === 'number') setPaymentAmount(cfg.price)
        })
        .catch(() => {})
    }
    return () => { cancelled = true }
  }, [])

  // --- Asset resolution ---
  const apiBase = import.meta.env.VITE_API_BASE_URL ?? ''
  const resolveAsset = (v: string) => {
    const s = (v ?? '').trim()
    if (!s) return ''
    if (/^https?:\/\//i.test(s)) return s
    return apiBase + s
  }

  // --- Favicon ---
  const faviconUrl = typeof (publicSiteConfig as Loose)?.faviconUrl === 'string' && (publicSiteConfig as Loose).faviconUrl.trim()
    ? resolveAsset((publicSiteConfig as Loose).faviconUrl)
    : '/favicon.svg'

  useEffect(() => {
    const link = document.querySelector("link[rel='icon']") as HTMLLinkElement | null
    if (link && faviconUrl) link.href = faviconUrl
    const appleLink = document.querySelector("link[rel='apple-touch-icon']") as HTMLLinkElement | null
    if (appleLink && faviconUrl) appleLink.href = faviconUrl
  }, [faviconUrl])

  // --- Headline font loading ---
  const headlineFont = ((publicSiteConfig as Loose)?.miscText?.headlineFont as string) || 'serif'
  useEffect(() => {
    if (headlineFont === 'serif' || headlineFont === 'sans-serif') return
    const id = `gfont-${headlineFont.replace(/\s+/g, '-')}`
    if (document.getElementById(id)) return
    const link = document.createElement('link')
    link.id = id
    link.rel = 'stylesheet'
    link.href = `https://fonts.googleapis.com/css2?family=${encodeURIComponent(headlineFont)}:wght@700&display=swap`
    document.head.appendChild(link)
  }, [headlineFont])

  // --- Hero video preview loop ---
  const heroPlayerPreview = (publicSiteConfig as Loose)?.landing?.heroPlayer
  const heroVideoPreviewLoop_ = Boolean(heroPlayerPreview?.videoPreviewLoop)
  const heroVideoPreviewLoopSeconds_ = Number(heroPlayerPreview?.videoPreviewLoopSeconds) || 5
  useEffect(() => {
    const v = heroVideoRef.current
    if (!v || !heroVideoPreviewLoop_ || heroOpen) return
    v.loop = false
    const handleTimeUpdate = () => {
      if (v.currentTime >= heroVideoPreviewLoopSeconds_) {
        v.currentTime = 0
      }
    }
    v.addEventListener('timeupdate', handleTimeUpdate)
    return () => {
      v.removeEventListener('timeupdate', handleTimeUpdate)
      v.loop = true
    }
  }, [heroVideoPreviewLoop_, heroVideoPreviewLoopSeconds_, heroOpen])

  // --- Relationship chips ---
  const heroRelChips = useMemo(() => {
    const cs = (publicSiteConfig as Loose)?.configSteps
    const s1 = cs?.step1 && typeof cs.step1 === 'object' ? cs.step1 : {}
    const chips: Array<{ label: string; icon: string; value: string }> = []
    if (Array.isArray(s1?.relationshipChips)) {
      for (const c of s1.relationshipChips) {
        if (c?.label) chips.push({ label: String(c.label), icon: typeof c.icon === 'string' ? c.icon.trim() : '', value: String(c.value || c.label) })
      }
    }
    if (!chips.length) {
      chips.push(
        { label: tr('Муж', 'Husband'), icon: '', value: 'Муж' },
        { label: tr('Жена', 'Wife'), icon: '', value: 'Жена' },
        { label: tr('Парень', 'Boyfriend'), icon: '', value: 'Парень' },
        { label: tr('Девушка', 'Girlfriend'), icon: '', value: 'Девушка' },
        { label: tr('Мама', 'Mom'), icon: '', value: 'Мама' },
        { label: tr('Папа', 'Dad'), icon: '', value: 'Папа' },
        { label: tr('Друг', 'Friend'), icon: '', value: 'Друг' },
        { label: tr('Другое', 'Other'), icon: '', value: 'Другое' },
      )
    }
    return chips
  }, [publicSiteConfig, tr])

  // ---------------------------------------------------------------------------
  // Derived / resolved data
  // ---------------------------------------------------------------------------

  const selectedDefault = withLangOverlay(design === 'gold' ? defaultPublicSiteConfigGold : defaultPublicSiteConfig, lang)
  const site = (publicSiteConfig ?? selectedDefault) as PublicSiteConfig

  // Theme style
  const themeColors = (publicSiteConfig as Loose)?.colors
  const themeStyle = {
    '--theme-accent': themeColors?.accentColor || '#E11D48',
    '--theme-button': themeColors?.buttonColor || themeColors?.accentColor || '#E11D48',
    '--theme-button-text': themeColors?.buttonTextColor || '#FFFFFF',
    '--theme-accent-text': themeColors?.accentTextColor || themeColors?.accentColor || '#E11D48',
    '--theme-accent-soft': themeColors?.bgColor1 || '#FFF5F7',
    '--theme-bg': themeColors?.bgColor2 || '#FFFFFF',
  } as React.CSSProperties

  // Logos
  const logoUrl = typeof (publicSiteConfig as Loose)?.logoUrl === 'string' && (publicSiteConfig as Loose).logoUrl.trim()
    ? resolveAsset((publicSiteConfig as Loose).logoUrl)
    : '/logo.svg'
  const mobileLogoUrl = typeof (publicSiteConfig as Loose)?.mobileLogoUrl === 'string' && (publicSiteConfig as Loose).mobileLogoUrl.trim()
    ? resolveAsset((publicSiteConfig as Loose).mobileLogoUrl)
    : logoUrl

  // Landing defaults
  const landing = site.landing ?? selectedDefault.landing!
  const heroMedia = landing.heroMedia ?? selectedDefault.landing!.heroMedia!
  const heroOverlay = landing.heroOverlay ?? selectedDefault.landing!.heroOverlay!
  const audioSamples = landing.audioSamples ?? selectedDefault.landing!.audioSamples!
  const heroPlayer = landing.heroPlayer ?? selectedDefault.landing!.heroPlayer!

  // Trust badges
  const trustBadge1 = site.trustBadges?.badge1 || tr(`Доставка ${deliveryEta.short}`, `Delivered in ${deliveryEta.short}`)
  const trustBadge2 = site.trustBadges?.badge2 || tr('Безопасно', 'Secure')
  const trustBadge3 = site.trustBadges?.badge3 || tr('Гарантия результата', 'Result guaranteed')

  // Stats bar
  const statsItems = (site.statsBar?.items && site.statsBar.items.length > 0)
    ? site.statsBar.items.map(s => ({ val: s.val || '', label: s.label || '' }))
    : [
        { val: '99%', label: tr('Тронули', 'Touched') },
        { val: deliveryEta.short, label: tr('Срок', 'Turnaround') },
        { val: '2 847', label: tr('Заказов', 'Orders') },
      ]

  // Promo banner
  const promoBannerRaw = site.promoBanner ?? {}
  const promoBanner: PromoBannerData = {
    enabled: promoBannerRaw.enabled !== false,
    countdownLabel: promoBannerRaw.countdownLabel || tr('\ud83d\udc9d До события', '\ud83d\udc9d Until the big day'),
    countdownTargetDate: promoBannerRaw.countdownTargetDate || '2027-02-14',
    promoBadgeText: promoBannerRaw.promoBadgeText || tr('\ud83d\udc9d Спецпредложение', '\ud83d\udc9d Special offer'),
    quotaBadgeText: promoBannerRaw.quotaBadgeText || '',
    evergreenEnabled: promoBannerRaw.evergreenEnabled ?? false,
    evergreenCycleHours: promoBannerRaw.evergreenCycleHours ?? 24,
  }

  // Reviews
  const reviewsSection = site.reviews ?? {}
  const reviewSectionLabel = reviewsSection.sectionLabel || tr('Настоящие реакции', 'Real reactions')
  const reviewSectionHeadline =
    reviewsSection.sectionHeadline || tr(
      '"Он <span class="text-[var(--theme-accent-text)] italic">никогда</span> не плачет"',
      '"He <span class="text-[var(--theme-accent-text)] italic">never</span> cries"',
    )
  const reviewSectionSubtext = reviewsSection.sectionSubtext || tr('…так отвечают почти все. В 98% случаев — не так.', '…that\'s what most people say. In 98% of cases, it isn\'t so.')
  const reviewItems = (reviewsSection.items && reviewsSection.items.length > 0) ? reviewsSection.items : defaultReviewItems

  // Hero headline
  const heroHeadlineLine1 =
    typeof landing.heroHeadline?.line1 === 'string' && landing.heroHeadline.line1.trim()
      ? landing.heroHeadline.line1
      : selectedDefault.landing!.heroHeadline!.line1!
  const heroHeadlineLine2 =
    typeof landing.heroHeadline?.line2 === 'string' && landing.heroHeadline.line2.trim()
      ? landing.heroHeadline.line2
      : selectedDefault.landing!.heroHeadline!.line2!
  const heroSubtext =
    typeof landing.heroSubtext === 'string' && landing.heroSubtext.trim()
      ? landing.heroSubtext
      : (selectedDefault.landing!.heroSubtext || tr(
          `Персональная песня с <strong>его/её именем</strong> в тексте. Срок ${deliveryEta.sentenceLower}.`,
          `A personalized song with <strong>their name</strong> in the lyrics. Delivered ${deliveryEta.sentenceLower}.`,
        ))

  // Footer CTA
  const footerCtaHeadline =
    typeof landing.footerCta?.headline === 'string' && landing.footerCta.headline.trim()
      ? landing.footerCta.headline
      : selectedDefault.landing!.footerCta!.headline!
  const footerCtaSubtitle =
    typeof landing.footerCta?.subtitle === 'string' && landing.footerCta.subtitle.trim()
      ? landing.footerCta.subtitle
      : selectedDefault.landing!.footerCta!.subtitle!

  // Hero media
  const heroImageUrl =
    typeof heroMedia.imageUrl === 'string' && heroMedia.imageUrl.trim()
      ? resolveAsset(heroMedia.imageUrl)
      : selectedDefault.landing!.heroMedia!.imageUrl!
  const heroVideoUrl =
    typeof heroMedia.videoUrl === 'string' && heroMedia.videoUrl.trim() ? resolveAsset(heroMedia.videoUrl) : null
  const heroVideoBelowPrice = (heroMedia as Loose).videoBelowPrice === true
  const heroVideoThumbnailUrl =
    typeof (heroMedia as Loose).videoThumbnailUrl === 'string' && (heroMedia as Loose).videoThumbnailUrl.trim()
      ? resolveAsset((heroMedia as Loose).videoThumbnailUrl)
      : null

  // Hero overlay
  const overlayQuote =
    typeof heroOverlay.quote === 'string' && heroOverlay.quote.trim()
      ? heroOverlay.quote
      : selectedDefault.landing!.heroOverlay!.quote!
  const overlayAuthorName =
    typeof heroOverlay.authorName === 'string' && heroOverlay.authorName.trim()
      ? heroOverlay.authorName
      : selectedDefault.landing!.heroOverlay!.authorName!
  const overlayAuthorLocation =
    typeof heroOverlay.authorLocation === 'string' && heroOverlay.authorLocation.trim()
      ? heroOverlay.authorLocation
      : selectedDefault.landing!.heroOverlay!.authorLocation!
  const overlayAuthorAvatarUrl =
    typeof heroOverlay.authorAvatarUrl === 'string' && heroOverlay.authorAvatarUrl.trim()
      ? resolveAsset(heroOverlay.authorAvatarUrl)
      : null

  // Hero player
  const heroPlayerEnabled = Boolean((heroPlayer as Loose)?.enabled)
  const heroPlayerAudioUrlRaw = (heroPlayer as Loose)?.audioUrl
  const nowPlayingAudioUrlRaw = (audioSamples.nowPlaying as Loose)?.audioUrl
  const nowPlayingAudioUrl =
    typeof nowPlayingAudioUrlRaw === 'string' && nowPlayingAudioUrlRaw.trim() ? resolveAsset(nowPlayingAudioUrlRaw) : null
  const heroPlayerAudioUrl =
    typeof heroPlayerAudioUrlRaw === 'string' && heroPlayerAudioUrlRaw.trim()
      ? resolveAsset(heroPlayerAudioUrlRaw)
      : nowPlayingAudioUrl
  const heroPlayerQuote =
    typeof (heroPlayer as Loose)?.quote === 'string' && String((heroPlayer as Loose).quote).trim()
      ? String((heroPlayer as Loose).quote).trim()
      : overlayQuote
  const heroPlayerAuthorName =
    typeof (heroPlayer as Loose)?.authorName === 'string' && String((heroPlayer as Loose).authorName).trim()
      ? String((heroPlayer as Loose).authorName).trim()
      : overlayAuthorName
  const heroPlayerAuthorSubline =
    typeof (heroPlayer as Loose)?.authorSubline === 'string' ? String((heroPlayer as Loose).authorSubline).trim() : ''
  const heroPlayerAvatarRaw = (heroPlayer as Loose)?.authorAvatarUrl
  const heroPlayerAvatarUrl =
    typeof heroPlayerAvatarRaw === 'string' && heroPlayerAvatarRaw.trim() ? resolveAsset(heroPlayerAvatarRaw) : overlayAuthorAvatarUrl
  const heroPlayingBadgeText =
    typeof (heroPlayer as Loose)?.playingBadgeText === 'string' && String((heroPlayer as Loose).playingBadgeText).trim()
      ? String((heroPlayer as Loose).playingBadgeText).trim()
      : tr('Сейчас', 'Now')
  const heroCornerBadgeText =
    typeof (heroPlayer as Loose)?.cornerBadgeText === 'string' && String((heroPlayer as Loose).cornerBadgeText).trim()
      ? String((heroPlayer as Loose).cornerBadgeText).trim()
      : tr('Спецпредложение', 'Special offer')
  const heroVerifiedBadgeText =
    typeof (heroPlayer as Loose)?.verifiedBadgeText === 'string' && String((heroPlayer as Loose).verifiedBadgeText).trim()
      ? String((heroPlayer as Loose).verifiedBadgeText).trim()
      : tr('Подтверждённый заказ', 'Verified order')
  const heroStopAfterOnePlay = Boolean((heroPlayer as Loose)?.stopAfterOnePlay)
  const heroUseVideoAudio = Boolean((heroPlayer as Loose)?.useVideoAudio)
  const heroStopCtaButtonText =
    typeof (heroPlayer as Loose)?.stopCtaButtonText === 'string' && String((heroPlayer as Loose).stopCtaButtonText).trim()
      ? String((heroPlayer as Loose).stopCtaButtonText).trim()
      : tr('Создать свою песню', 'Create your song')

  // Now playing & playlist
  const nowPlaying = audioSamples.nowPlaying ?? selectedDefault.landing!.audioSamples!.nowPlaying!
  const nowPlayingName =
    typeof nowPlaying.name === 'string' && nowPlaying.name.trim()
      ? nowPlaying.name
      : selectedDefault.landing!.audioSamples!.nowPlaying!.name!
  const nowPlayingQuote =
    typeof nowPlaying.quote === 'string' && nowPlaying.quote.trim()
      ? nowPlaying.quote
      : selectedDefault.landing!.audioSamples!.nowPlaying!.quote!
  const nowPlayingTime =
    typeof nowPlaying.time === 'string' && nowPlaying.time.trim()
      ? nowPlaying.time
      : selectedDefault.landing!.audioSamples!.nowPlaying!.time!
  const nowPlayingMetaTextRaw = (nowPlaying as Loose).metaText
  const nowPlayingMetaText =
    typeof nowPlayingMetaTextRaw === 'string'
      ? nowPlayingMetaTextRaw.trim() || null
      : (selectedDefault.landing!.audioSamples!.nowPlaying! as Loose).metaText ?? null

  const playlist =
    Array.isArray(audioSamples.playlist) && audioSamples.playlist.length > 0
      ? audioSamples.playlist
      : selectedDefault.landing!.audioSamples!.playlist!

  const allTracks: TrackItem[] = [
    {
      title: nowPlayingName,
      subtitle: nowPlayingMetaText ?? '',
      quote: nowPlayingQuote,
      time: nowPlayingTime,
      audioUrl: nowPlayingAudioUrl,
      thumbnailUrl: typeof nowPlaying.thumbnailUrl === 'string' && nowPlaying.thumbnailUrl.trim() ? resolveAsset(nowPlaying.thumbnailUrl.trim()) : null,
    },
    ...playlist.map((p: Loose) => {
      const sub = typeof p.subtitle === 'string' ? p.subtitle : ''
      const timeMatch = sub.match(/(\d+:\d{2})$/)
      return {
        title: p.title ?? '',
        subtitle: sub,
        quote: '',
        time: timeMatch ? timeMatch[1] : '--:--',
        audioUrl: typeof p.audioUrl === 'string' && p.audioUrl.trim() ? resolveAsset(p.audioUrl.trim()) : null,
        thumbnailUrl: typeof p.thumbnailUrl === 'string' && p.thumbnailUrl.trim() ? resolveAsset(p.thumbnailUrl.trim()) : null,
      }
    }),
  ]

  // Section order & visibility
  const sectionOrder = resolveSectionOrder((site as Loose).sectionOrder)
  const sectionVis = resolveSectionVisibility({
    sectionVisibility: (site as Loose).sectionVisibility,
    howItWorksSection: site.howItWorksSection,
    faqSection: site.faqSection,
  })

  // Price visibility
  const pv = site.priceVisibility ?? {} as Loose
  const showPrice: ShowPrice = {
    promoBanner: pv.promoBanner !== false,
    header: pv.header !== false,
    heroCtaButton: pv.heroCtaButton !== false,
    audioSamplesButton: pv.audioSamplesButton !== false,
    comparisonSection: pv.comparisonSection !== false,
    howItWorksButton: pv.howItWorksButton !== false,
    footerCtaButton: pv.footerCtaButton !== false,
    mobileStickyButton: pv.mobileStickyButton !== false,
  }

  // Toast config
  const toastConfig = (site.activityToast && typeof site.activityToast === 'object'
    ? site.activityToast
    : selectedDefault.activityToast) as ActivityToastConfig

  const miscText = site.miscText

  // ---------------------------------------------------------------------------
  // Return
  // ---------------------------------------------------------------------------

  return {
    themeSlug,
    design,
    themeStyle,
    logoUrl,
    mobileLogoUrl,

    fmtCurrency,
    paymentAmount,
    originalAmount,
    deliveryEta,
    showPrice,

    promoBanner,

    hero: {
      headlineLine1: heroHeadlineLine1,
      headlineLine2: heroHeadlineLine2,
      subtext: heroSubtext,
      imageUrl: heroImageUrl,
      videoUrl: heroVideoUrl,
      mediaMode: (heroMedia.mode === 'video' ? 'video' : 'image') as 'image' | 'video',
      videoBelowPrice: heroVideoBelowPrice,
      videoThumbnailUrl: heroVideoThumbnailUrl,
      headlineFont,
      overlay: {
        quote: overlayQuote,
        authorName: overlayAuthorName,
        authorLocation: overlayAuthorLocation,
        authorAvatarUrl: overlayAuthorAvatarUrl,
      },
      player: {
        enabled: heroPlayerEnabled,
        audioUrl: heroPlayerAudioUrl,
        playingBadgeText: heroPlayingBadgeText,
        cornerBadgeText: heroCornerBadgeText,
        verifiedBadgeText: heroVerifiedBadgeText,
        quote: heroPlayerQuote,
        authorName: heroPlayerAuthorName,
        authorSubline: heroPlayerAuthorSubline,
        authorAvatarUrl: heroPlayerAvatarUrl,
        stopAfterOnePlay: heroStopAfterOnePlay,
        useVideoAudio: heroUseVideoAudio,
        stopCtaButtonText: heroStopCtaButtonText,
      },
      chips: heroRelChips,
      trustBadges: { badge1: trustBadge1, badge2: trustBadge2, badge3: trustBadge3 },
    },

    miscText,

    sectionOrder,
    sectionVisibility: sectionVis,

    sections: {
      audioSamples: site.audioSamplesSection,
      comparison: site.comparisonSection,
      howItWorks: site.howItWorksSection,
      guarantee: site.guaranteeSection,
      faq: site.faqSection,
      footer: site.footer,
      footerCta: {
        headline: footerCtaHeadline,
        subtitle: footerCtaSubtitle,
        securityBadge: landing.footerCta?.securityBadge,
        quotaLine: landing.footerCta?.quotaLine,
      },
      reviews: {
        sectionLabel: reviewSectionLabel,
        sectionHeadline: reviewSectionHeadline,
        sectionSubtext: reviewSectionSubtext,
        items: reviewItems,
      },
      statsBar: { items: statsItems },
      faqItems,
    },

    allTracks,
    toastConfig,

    heroOpen,
    setHeroOpen,
    heroVideoPlaying,
    setHeroVideoPlaying,
    selectedTrackIndex,
    setSelectedTrackIndex,
    autoPlayOnSelect,
    setAutoPlayOnSelect,
    showMobileCta,
    videoReady,

    heroRef,
    heroVideoRef,

    sanitizeHtml,
    resolveAsset,
  }
}
