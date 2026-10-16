import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useForm, type FieldErrors, type Path } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { OrderInputSchema, type OrderInput } from 'shared'
import { apiGet, apiPost } from '@/lib/http'
import { cachedSettingsGet } from '@/lib/settingsCache'
import { getAttribution } from '@/lib/attribution'
import type { Loose } from '@/lib/loose'
import { useThemeSlug } from '@/features/theme/ThemeContext'
import { ensureMetaPixelLoaded, trackWishlist, executePixelScript, trackPixelEvent } from '@/features/analytics/MetaPixel'
import { executeTikTokScript, trackTikTokEvent } from '@/features/analytics/TikTokPixel'

import { CountdownTimer } from '@/components/landing/CountdownTimer'
import { SeekBar } from '@/components/landing/SeekBar'
import { ReactionVideosSection } from '@/components/landing/ReactionVideosSection'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { SelectionChip } from '@/components/ui/selection-chip'
import { genderFromRelationship, type RecipientGender } from '@/lib/recipientGender'
import { VibeCard } from '@/components/ui/vibe-card'
import { PromptChip } from '@/components/ui/prompt-chip'
import { Separator } from '@/components/ui/separator'
import { DraftCountdown } from './config/DraftCountdown'
import {
  type PersistedConfigDraft,
  CONFIG_DRAFT_STORAGE_KEY,
  fmtCurrency,
  getErrorMessage,
  getPersistableFormValues,
  hasMeaningfulDraft,
  generateDraftKey,
  resolveAsset,
  ruDaysPhrase,
  ruHoursPhrase,
} from './config/utils'

import {
  Heart,
  Music,
  PenLine,
  ShieldCheck,
  Clock,
  ChevronLeft,
  Sparkles,
  Zap,
  PartyPopper,
  Mail,
  Loader2,
  Check,
  Megaphone,
  ChevronDown,
  Play,
  Pause,
  ChevronRight,
  BadgeCheck,
  Tag,
  Clipboard
} from 'lucide-react'

export function ConfigRoute({ adminMode = false, subscriberMode = false }: { adminMode?: boolean; subscriberMode?: boolean } = {}) {
  // Подписчик (и админ) не платит за песню — вся ценовая часть формы прячется.
  const noPaymentMode = adminMode || subscriberMode
  const themeSlug = useThemeSlug()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [isHydrated, setIsHydrated] = useState(false)
  const [step, setStep] = useState(0)
  const [loading, setLoading] = useState(false)
  const [navigatingToPayment, setNavigatingToPayment] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [relationship, setRelationship] = useState('Партнёр')
  // Пол получателя. Для «Муж», «Жена», «Мама» и прочих однозначных чипов он
  // выводится из отношения; спрашиваем только там, где слово пола не задаёт
  // («Друг», «Другое»), иначе форма задавала бы вопрос с известным ответом.
  const [pickedGender, setPickedGender] = useState<RecipientGender | null>(null)
  const storyTextareaRef = useRef<HTMLTextAreaElement | null>(null)
  const emailSectionRef = useRef<HTMLDivElement | null>(null)
  const [instantEnabled, setInstantEnabled] = useState<boolean | null>(null)
  const [deliveryDelayHours, setDeliveryDelayHours] = useState<number | null>(null)
  const [manualConfirmationEnabled, setManualConfirmationEnabled] = useState(false)
  const [faqOpenIndex, setFaqOpenIndex] = useState<number | null>(null)
  const [playingTrackIdx, setPlayingTrackIdx] = useState<number | null>(null)
  const [miniProgress, setMiniProgress] = useState(0)
  const miniPlayerRef = useRef<HTMLAudioElement | null>(null)


  const [paymentAmount, setPaymentAmount] = useState<number>(2990)
  const [originalAmount, setOriginalAmount] = useState<number>(2990)
  const [wishlistPixelId, setWishlistPixelId] = useState<string | null>(null)
  const [pixelStep1Script, setPixelStep1Script] = useState<string | null>(null)
  const [pixelStep4Script, setPixelStep4Script] = useState<string | null>(null)
  const pixelStep1Fired = useRef(false)
  const pixelStep4Fired = useRef(false)
  const [tiktokStep1Script, setTiktokStep1Script] = useState<string | null>(null)
  const [tiktokStep4Script, setTiktokStep4Script] = useState<string | null>(null)
  const tiktokStep1Fired = useRef(false)
  const tiktokStep4Fired = useRef(false)
  const styleStepGtmFired = useRef(false)
  const [discountCodeInput, setDiscountCodeInput] = useState('')
  const [discountValidating, setDiscountValidating] = useState(false)
  const [discountError, setDiscountError] = useState<string | null>(null)
  const [appliedDiscount, setAppliedDiscount] = useState<{ code: string; type: 'fixed' | 'percentage'; amount?: number; percentageValue?: number } | null>(null)
  const [discountExpanded, setDiscountExpanded] = useState(false)

  const deliveryEta = useMemo(() => {
    if (instantEnabled) {
      return { label: '10–30 мин', sentenceLower: 'за 10–30 минут', short: '10–30 мин' }
    }
    const hRaw = deliveryDelayHours
    const h = typeof hRaw === 'number' && Number.isFinite(hRaw) && hRaw > 0 ? hRaw : 24
    if (h >= 24 && h % 24 === 0) {
      const d = h / 24
      const phrase = ruDaysPhrase(d)
      return { label: `За ${phrase}`, sentenceLower: `за ${phrase}`, short: phrase }
    }
    const phrase = ruHoursPhrase(h)
    return { label: `За ${phrase}`, sentenceLower: `за ${phrase}`, short: phrase }
  }, [deliveryDelayHours, instantEnabled])

  // Reset loading state when page is restored from bfcache (e.g. browser back from Xendit)
  useEffect(() => {
    const handler = (e: PageTransitionEvent) => {
      if (e.persisted) {
        setLoading(false)
        // Delay resetting navigatingToPayment so bfcache flash shows spinner, not the form
        setTimeout(() => setNavigatingToPayment(false), 500)
      }
    }
    window.addEventListener('pageshow', handler)
    return () => window.removeEventListener('pageshow', handler)
  }, [])

  useEffect(() => {
    let cancelled = false
    cachedSettingsGet<{
      emailEnabled?: boolean
      emailOtpEnabled?: boolean
      agreementEnabled?: boolean
      publicSiteConfig?: unknown
      instantEnabled?: boolean
      deliveryDelayHours?: number
      manualConfirmationEnabled?: boolean
      paymentAmount?: number
      originalAmount?: number
      metaPixelWishlistId?: string | null
      metaPixelStep1Script?: string | null
      metaPixelStep4Script?: string | null
      tiktokPixelStep1Script?: string | null
      tiktokPixelStep4Script?: string | null
    }>(`/api/public/settings${themeSlug ? `?theme=${encodeURIComponent(themeSlug)}` : ''}`)
      .then((res) => {
        if (cancelled) return
        setEmailEnabled(res?.emailEnabled ?? true)
        setEmailOtpEnabled(res?.emailOtpEnabled ?? true)
        setAgreementEnabled(res?.agreementEnabled ?? false)
        setInstantEnabled(typeof res?.instantEnabled === 'boolean' ? res.instantEnabled : null)
        setDeliveryDelayHours(typeof res?.deliveryDelayHours === 'number' ? res.deliveryDelayHours : null)
        setManualConfirmationEnabled(Boolean(res?.manualConfirmationEnabled))
        setPaymentAmount(res?.paymentAmount ?? 2990)
        setOriginalAmount(res?.originalAmount ?? 2990)
        setWishlistPixelId(res?.metaPixelWishlistId ?? null)
        setPixelStep1Script(res?.metaPixelStep1Script ?? null)
        setPixelStep4Script(res?.metaPixelStep4Script ?? null)
        setTiktokStep1Script(res?.tiktokPixelStep1Script ?? null)
        setTiktokStep4Script(res?.tiktokPixelStep4Script ?? null)
        
        // Resolve hero video URL
        type PublicSiteConfigMaybe = { landing?: { heroMedia?: { videoUrl?: unknown } } }
        const publicSiteConfig = res?.publicSiteConfig
        const landing =
          publicSiteConfig && typeof publicSiteConfig === 'object'
            ? (publicSiteConfig as PublicSiteConfigMaybe).landing
            : undefined
        const heroMedia = landing?.heroMedia
        const rawVideoUrl = heroMedia?.videoUrl
        if (typeof rawVideoUrl === 'string' && rawVideoUrl.trim()) {
           const s = rawVideoUrl.trim()
           const apiBase = import.meta.env.VITE_API_BASE_URL ?? ''
           const resolved = /^https?:\/\//i.test(s) ? s : apiBase + s
           setHeroVideoUrl(resolved)
        }

        setPublicSiteConfig(publicSiteConfig ?? null)
        setSettingsLoaded(true)
      })
      .catch(() => {
        if (!cancelled) {
          setPublicSiteConfig(null)
          setEmailOtpEnabled(true)
          setSettingsLoaded(true)
        }
      })
    return () => { cancelled = true }
  }, [])

  // Email field and OTP verification state for checkout step
  const [emailEnabled, setEmailEnabled] = useState(true)
  const [emailOtpEnabled, setEmailOtpEnabled] = useState(true)
  const [agreementEnabled, setAgreementEnabled] = useState(false)
  const [settingsLoaded, setSettingsLoaded] = useState(false)
  const [heroVideoUrl, setHeroVideoUrl] = useState<string | null>(null)
  const [publicSiteConfig, setPublicSiteConfig] = useState<unknown>(undefined)

  const configSteps = useMemo(() => {
    const cfg = publicSiteConfig && typeof publicSiteConfig === 'object' ? (publicSiteConfig as Loose) : {}
    const cs = cfg?.configSteps && typeof cfg.configSteps === 'object' ? cfg.configSteps : {}
    const s0 = cs?.step0 && typeof cs.step0 === 'object' ? cs.step0 : {}
    const s1 = cs?.step1 && typeof cs.step1 === 'object' ? cs.step1 : {}
    const s2 = cs?.step2 && typeof cs.step2 === 'object' ? cs.step2 : {}
    const s3 = cs?.step3 && typeof cs.step3 === 'object' ? cs.step3 : {}
    const pkg = cs?.packageStep && typeof cs.packageStep === 'object' ? cs.packageStep : {}
    const s4 = cs?.step4 && typeof cs.step4 === 'object' ? cs.step4 : {}

    const str = (v: unknown, fb: string) => typeof v === 'string' && v.trim() ? v : fb
    const arr = <T,>(v: unknown, map: (x: Loose) => T): T[] => Array.isArray(v) ? v.map(map) : []

    const howSteps = arr(s0?.howItWorksSteps, (x: Loose) => ({
      title: str(x?.title, ''),
      subtitle: str(x?.subtitle, ''),
    })).filter((x: Loose) => x.title)

    const relChips = arr(s1?.relationshipChips, (x: Loose) => ({
      label: str(x?.label, ''),
      icon: typeof x?.icon === 'string' ? x.icon.trim() : '',
      value: str(x?.value, ''),
    })).filter((x: Loose) => x.label && x.value)

    const vibeChips = arr(s2?.vibeChips, (x: Loose) => ({
      id: str(x?.id, ''),
      label: str(x?.label, ''),
      desc: str(x?.desc, ''),
      icon: typeof x?.icon === 'string' ? x.icon.trim() : '',
      badge: x?.badge ? str(x.badge, '') : undefined,
    })).filter((x: Loose) => x.id && x.label)

    const voiceOptions = arr(s2?.voiceOptions, (x: Loose) => ({
      value: str(x?.value, ''),
      label: str(x?.label, ''),
    })).filter((x: Loose) => x.value && x.label)

    const languageOptions = arr(s2?.languageOptions, (x: Loose) => ({
      value: str(x?.value, ''),
      label: str(x?.label, ''),
    })).filter((x: Loose) => x.value && x.label)

    const tipBullets = arr(s3?.tipBullets, (x: unknown) => typeof x === 'string' ? x : '').filter((x: string) => x.trim())

    const storyPrompts = arr(s3?.storyPrompts, (x: Loose) => ({
      label: str(x?.label, ''),
      icon: str(x?.icon, '💡'),
    })).filter((x: Loose) => x.label)

    const nextSteps = arr(s4?.nextSteps, (x: Loose) => ({ text: str(x?.text, '') })).filter((x: Loose) => x.text)
    const manualNextSteps = arr(s4?.manualNextSteps, (x: Loose) => ({ text: str(x?.text, '') })).filter((x: Loose) => x.text)
    const securityBadges = arr(s4?.securityBadges, (x: unknown) => typeof x === 'string' ? x : '').filter((x: string) => x.trim())

    return {
      step0: {
        enabled: typeof s0?.enabled === 'boolean' ? s0.enabled : false,
        bannerHeadline: str(s0?.bannerHeadline, 'Самый романтичный подарок'),
        mainHeadline: str(
          s0?.mainHeadline,
          'Снимите эмоции, когда он/она услышит песню — и участвуйте в розыгрыше <span class="text-[var(--theme-accent-text)]">100 000 ₽</span>',
        ),
        guaranteeTitle: str(s0?.guaranteeTitle, 'ДЕНЕЖНАЯ ГАРАНТИЯ'),
        guaranteeText: str(
          s0?.guaranteeText,
          'За каждое чёткое видео с реакцией <span class="font-bold underline decoration-green-500/50">гарантированно 7 500 ₽</span>.',
        ),
        howItWorksTitle: str(s0?.howItWorksTitle, 'Как это работает'),
        howItWorksSteps: howSteps.length ? howSteps : [
          { title: 'Создайте песню', subtitle: 'Скидка до 100%' },
          { title: 'Снимите реакцию', subtitle: 'Нужно видео' },
          { title: 'Отправьте и выиграйте', subtitle: 'Призы' },
        ],
        bottomCtaText: str(s0?.bottomCtaText, 'Нажмите кнопку ниже, чтобы начать 👇'),
      },
      step1: {
        headline: str(s1?.headline, 'Кому дарим?'),
        subtitle: str(s1?.subtitle, 'Имя попадёт в текст песни'),
        relationshipChips: relChips.length ? relChips : [
          { label: 'Муж', icon: '', value: 'Муж' },
          { label: 'Жена', icon: '', value: 'Жена' },
          { label: 'Парень', icon: '', value: 'Парень' },
          { label: 'Девушка', icon: '', value: 'Девушка' },
          { label: 'Мама', icon: '', value: 'Мама' },
          { label: 'Папа', icon: '', value: 'Папа' },
          { label: 'Друг', icon: '', value: 'Друг' },
        ],
        nameFieldLabel: str(s1?.nameFieldLabel, 'Как его/её зовут'),
        nameFieldPlaceholder: str(s1?.nameFieldPlaceholder, 'напр. Алекс'),
        occasionFieldLabel: str(s1?.occasionFieldLabel, 'Повод?'),
        occasionFieldPlaceholder: str(s1?.occasionFieldPlaceholder, 'Годовщина, день рождения…'),
        reactionVideosEnabled: typeof s1?.reactionVideosEnabled === 'boolean' ? s1.reactionVideosEnabled : false,
        reactionVideosHeadline: str(s1?.reactionVideosHeadline, 'Посмотрите, как близкие реагируют, когда слышат песню впервые.'),
        reactionVideosThumbnails: arr(s1?.reactionVideosThumbnails, (x: unknown) => typeof x === 'string' ? x : '').filter((x: string) => x.trim()),
        reactionVideosVideoUrl: str(s1?.reactionVideosVideoUrl, ''),
        socialProofText: str(s1?.socialProofText, 'К нам присоединились тысячи людей, которые сделали незабываемый подарок для своих близких'),
      },
      step2: {
        headline: str(s2?.headline, 'Выберите стиль'),
        subtitle: str(s2?.subtitle, 'Подберите под любимую музыку'),
        vibeChips: vibeChips.length ? vibeChips : [
          { id: 'Russian Rap', label: 'Русский рэп', desc: 'Честно и по-уличному', icon: '' },
          { id: 'Acoustic', label: 'Акустика', desc: 'Тепло и камерность', icon: '' },
          { id: 'Pop Ballad', label: 'Поп-баллада', desc: 'Современно и проникновенно', icon: '', badge: 'Хит' },
          { id: 'R&B Soul', label: 'R&B Soul', desc: 'Плавно и романтично', icon: '' },
          { id: 'Rock', label: 'Рок', desc: 'Мощно и страстно', icon: '' },
          { id: 'Piano Ballad', label: 'Фортепиано', desc: 'Элегантно и вечно', icon: '' },
          { id: 'Chanson', label: 'Шансон', desc: 'Душевно и по-настоящему', icon: '' },
        ],
        voiceLabel: str(s2?.voiceLabel, 'Голос'),
        voiceOptions: voiceOptions.length ? voiceOptions : [
          { value: 'Female', label: 'Женский' },
          { value: 'Male', label: 'Мужской' },
          { value: 'Surprise me', label: 'Как получится' },
        ],
        languageLabel: str(s2?.languageLabel, 'Язык текста'),
        languageOptions: languageOptions.length ? languageOptions : [
          { value: 'Russian', label: 'Русский' },
          { value: 'English', label: 'Английский' },
        ],
      },
      step3: {
        headline: str(s3?.headline, 'Расскажите историю'),
        subtitle: str(
          s3?.subtitle,
          'Из этого сделаем текст песни. <span class="text-[var(--theme-accent-text)] font-medium">Хватит пары предложений!</span>',
        ),
        tipBullets: tipBullets.length ? tipBullets : [
          'Чем больше деталей — тем сильнее эмоции.',
          'Встреча, что цените вместе или смешные моменты.',
        ],
        storyPrompts: storyPrompts.length ? storyPrompts : [
          { label: 'Как познакомились', icon: '' },
          { label: 'Что я люблю в нём/ней', icon: '' },
          { label: '«Наши» шутки', icon: '' },
          { label: 'Мечты на будущее', icon: '' },
        ],
        textareaPlaceholder: str(s3?.textareaPlaceholder, 'Начните вводить текст здесь…'),
      },
      packageStep: {
        enabled: typeof pkg?.enabled === 'boolean' ? pkg.enabled : false,
        headline: str(pkg?.headline, 'Выберите пакет'),
        subtitle: str(pkg?.subtitle, 'Тот, который вам ближе'),
        displayOrder: (pkg?.displayOrder === 'base-first' || pkg?.displayOrder === 'bundle-first') ? pkg.displayOrder : 'bundle-first',
        basePackage: {
          name: str(pkg?.basePackage?.name, 'Стандарт'),
          description: str(pkg?.basePackage?.description, ''),
          bullets: arr(pkg?.basePackage?.bullets, (x: Loose) => ({ text: str(x?.text, '') })).filter((x: { text: string }) => x.text).slice(0, 3),
          ctaText: str(pkg?.basePackage?.ctaText, 'Этот пакет'),
        },
        bundlePackage: {
          name: str(pkg?.bundlePackage?.name, 'Выгодный'),
          description: str(pkg?.bundlePackage?.description, ''),
          bullets: arr(pkg?.bundlePackage?.bullets, (x: Loose) => ({ text: str(x?.text, '') })).filter((x: { text: string }) => x.text).slice(0, 3),
          ctaText: str(pkg?.bundlePackage?.ctaText, 'Выбрать выгодный'),
          linkedUpsellIds: Array.isArray(pkg?.bundlePackage?.linkedUpsellIds)
            ? (pkg.bundlePackage.linkedUpsellIds as unknown[]).filter((x: unknown): x is string => typeof x === 'string' && Boolean(x))
            : typeof pkg?.bundlePackage?.linkedUpsellId === 'string' && pkg.bundlePackage.linkedUpsellId
              ? [pkg.bundlePackage.linkedUpsellId]
              : [],
        },
      },
      step4: {
        headline: str(s4?.headline, 'Всё готово!'),
        subtitleTemplate: str(s4?.subtitleTemplate, 'Куда отправить песню для {recipient}?'),
        manualSubtitle: str(s4?.manualSubtitle, 'Подтверждение заказа'),
        orderSummaryLabel: str(s4?.orderSummaryLabel, 'Сводка заказа'),
        orderSummaryEmoji: typeof s4?.orderSummaryEmoji === 'string' ? s4.orderSummaryEmoji.trim() : '',
        emailLabel: str(s4?.emailLabel, 'Email'),
        emailPlaceholder: 'Ваш email',
        nextStepsTitle: str(s4?.nextStepsTitle, 'Что дальше?'),
        nextSteps: nextSteps.length ? nextSteps : [
          { text: 'Мы создаём вашу персональную песню' },
          { text: 'Отправим на электронную почту {delivery}' },
          { text: 'Включите близкому — и сохраните этот момент' },
        ],
        manualNextSteps: manualNextSteps.length ? manualNextSteps : [
          { text: 'Менеджер проверит заказ' },
          { text: 'Менеджер свяжется с вами для подтверждения' },
          { text: 'Включите близкому — и сохраните этот момент' },
        ],
        securityBadges: (() => {
          const resolved = securityBadges.length ? securityBadges : ['Безопасная оплата', '{delivery}']
          // Admin/subscriber mode has no payment — drop the "Безопасная оплата" badge.
          return noPaymentMode ? resolved.filter((b: string) => !/оплат/i.test(b)) : resolved
        })(),
        draftTimerText: str(s4?.draftTimerText, 'Черновик хранится {timer} — завершите заказ, чтобы сохранить место'),
        checkoutButtonText: noPaymentMode ? 'Получить песню' : str(s4?.checkoutButtonText, 'К оплате'),
        checkoutSubtext: str(s4?.checkoutSubtext, 'Готовы сделать особенный подарок для {recipient}?'),
        manualCheckoutButtonText: str(s4?.manualCheckoutButtonText, 'Подтвердить заказ'),
        showPriceInButton: typeof s4?.showPriceInButton === 'boolean' ? s4.showPriceInButton : true,
        checkoutImageUrl: str(s4?.checkoutImageUrl, '/images/checkout-studio.png'),
        showCheckoutImage: typeof s4?.showCheckoutImage === 'boolean' ? s4.showCheckoutImage : true,
      },
    }
  }, [publicSiteConfig, noPaymentMode])

  const funnelPriceVisibility = useMemo(() => {
    // Admin/subscriber mode bypasses payment entirely — hide every price (header, button, summary).
    if (noPaymentMode) {
      return { funnelHeader: false, orderSummary: false, checkoutButton: false }
    }
    const cfg = publicSiteConfig && typeof publicSiteConfig === 'object' ? (publicSiteConfig as Loose) : {}
    const pv = cfg?.priceVisibility && typeof cfg.priceVisibility === 'object' ? cfg.priceVisibility : {}
    return {
      funnelHeader: pv.funnelHeader !== false,
      orderSummary: pv.orderSummary !== false,
      checkoutButton: pv.checkoutButton !== false,
    }
  }, [publicSiteConfig, noPaymentMode])

  const checkoutExtraData = useMemo(() => {
    const cfg = publicSiteConfig && typeof publicSiteConfig === 'object' ? (publicSiteConfig as Loose) : {}

    const faqRaw = cfg?.faqSection
    const faqItems: Array<{ q: string; a: string }> = []
    if (faqRaw?.items && Array.isArray(faqRaw.items)) {
      for (const it of faqRaw.items) {
        if (it?.q && it?.a) faqItems.push({ q: String(it.q), a: String(it.a) })
      }
    }
    if (!faqItems.length) {
      faqItems.push(
        { q: 'Он/она «не эмоциональный человек»…', a: 'Так говорят почти все 😊 98% реагируют очень сильно — чем строже фасад, тем глубже трогает. 😉' },
        { q: 'Как близкий получит трек?', a: 'Вы получите ссылку на скачивание на email. Включите вслух или устройте сюрприз.' },
        { q: 'Что если результат не понравится?', a: 'Бесплатные правки, пока не станет близко к идеалу. Если совсем не зайдёт — полный возврат без вопросов. 💕' },
      )
    }

    const gSec = cfg?.guaranteeSection
    const guarantee = {
      badge: typeof gSec?.badge === 'string' && gSec.badge.trim() ? gSec.badge : '100% спокойствия',
      headline: typeof gSec?.headline === 'string' && gSec.headline.trim() ? gSec.headline : 'Гарантия качества',
      description:
        typeof gSec?.description === 'string' && gSec.description.trim()
          ? gSec.description
          : 'Неограниченные правки. Не подошло — возврат 100% без лишних вопросов.',
    }

    const landing = cfg?.landing && typeof cfg.landing === 'object' ? cfg.landing : {}
    const as_ = landing?.audioSamples && typeof landing.audioSamples === 'object' ? landing.audioSamples : {}
    const playlist: Array<{ title: string; subtitle: string; audioUrl: string }> = []
    const np = as_?.nowPlaying
    if (np && typeof np === 'object' && typeof np.name === 'string' && np.name.trim()) {
      playlist.push({
        title: String(np.name),
        subtitle: typeof np.time === 'string' ? np.time : '',
        audioUrl: typeof np.audioUrl === 'string' ? np.audioUrl : '',
      })
    }
    if (Array.isArray(as_?.playlist)) {
      for (const t of as_.playlist) {
        if (t?.title) {
          playlist.push({
            title: String(t.title),
            subtitle: typeof t.subtitle === 'string' ? t.subtitle : '',
            audioUrl: typeof t.audioUrl === 'string' ? t.audioUrl : '',
          })
        }
      }
    }

    const pb = cfg?.promoBanner && typeof cfg.promoBanner === 'object' ? cfg.promoBanner : {}
    const promoBanner = {
      enabled: pb.enabled !== false,
      promoBadgeText: typeof pb.promoBadgeText === 'string' && pb.promoBadgeText.trim() ? pb.promoBadgeText : 'Акция',
      countdownLabel: typeof pb.countdownLabel === 'string' && pb.countdownLabel.trim() ? pb.countdownLabel : '💝 До конца акции',
      countdownTargetDate: typeof pb.countdownTargetDate === 'string' && pb.countdownTargetDate.trim() ? pb.countdownTargetDate : '2027-02-14',
      evergreenEnabled: pb.evergreenEnabled ?? false,
      evergreenCycleHours: typeof pb.evergreenCycleHours === 'number' ? pb.evergreenCycleHours : 24,
    }

    return { faqItems, guarantee, playlist, promoBanner }
  }, [publicSiteConfig])

  const upsellConfig = useMemo(() => {
    const cfg = publicSiteConfig && typeof publicSiteConfig === 'object' ? (publicSiteConfig as Loose) : {}
    const u = cfg?.upsell && typeof cfg.upsell === 'object' ? cfg.upsell : {}
    if (!u.enabled) return null
    const items = Array.isArray(u.items) ? u.items.filter((it: Loose) => it?.id && it?.title && !it?.orderProcessingOnly) : []
    if (items.length === 0) return null
    return {
      headline: typeof u.headline === 'string' ? u.headline : '',
      footerNote: typeof u.footerNote === 'string' ? u.footerNote : '',
      items: items.map((it: Loose) => ({
        id: String(it.id),
        icon: typeof it.icon === 'string' ? it.icon : '',
        title: String(it.title),
        headline: typeof it.headline === 'string' ? it.headline : '',
        description: typeof it.description === 'string' ? it.description : '',
        price: typeof it.price === 'number' ? it.price : 0,
        priceLabel: typeof it.priceLabel === 'string' ? it.priceLabel : '',
        ctaText: typeof it.ctaText === 'string' ? it.ctaText : '',
        declineText: typeof it.declineText === 'string' ? it.declineText : '',
      })),
    }
  }, [publicSiteConfig])

  // Package step: derive whether it's active and the linked upsell items.
  // Look up from raw publicSiteConfig (not filtered upsellConfig) so the package step
  // works even when the upsell carousel itself is disabled.
  const linkedUpsellItems = useMemo(() => {
    if (!configSteps.packageStep.enabled || configSteps.packageStep.bundlePackage.linkedUpsellIds.length === 0) return []
    const cfg = publicSiteConfig && typeof publicSiteConfig === 'object' ? (publicSiteConfig as Loose) : {}
    const rawItems = Array.isArray(cfg?.upsell?.items) ? cfg.upsell.items : []
    return configSteps.packageStep.bundlePackage.linkedUpsellIds
      .map((uid: string) => {
        const match = rawItems.find((it: Loose) => it?.id === uid)
        if (!match) return null
        return {
          id: String(match.id),
          price: typeof match.price === 'number' ? match.price : 0,
        }
      })
      .filter(Boolean) as { id: string; price: number }[]
  }, [configSteps.packageStep, publicSiteConfig])

  const packageStepEnabled = configSteps.packageStep.enabled && linkedUpsellItems.length > 0
  const CHECKOUT_STEP = packageStepEnabled ? 5 : 4
  const TOTAL_STEPS = CHECKOUT_STEP
  const PACKAGE_STEP = 4

  // Normalize step number for analytics: the package step is an optional intermediate
  // step that shouldn't shift funnel analytics. Step 4 in analytics always = checkout.
  // Returns null for the package step (skip draft sync — it's a transient selection screen).
  const getAnalyticsStep = (s: number): number | null => {
    if (!packageStepEnabled) return s
    // Package step is transient — don't update the draft step number
    if (s === PACKAGE_STEP) return null
    // Checkout step (5) maps to 4 in analytics
    if (s === CHECKOUT_STEP) return 4
    return s
  }

  const [selectedPackageUpsellIds, setSelectedPackageUpsellIds] = useState<string[]>([])

  useEffect(() => {
    if (settingsLoaded && !configSteps.step0.enabled && step === 0) {
      setStep(1)
    }
  }, [settingsLoaded, configSteps.step0.enabled])

  // Clamp step when package step is disabled but step is beyond old max
  useEffect(() => {
    if (settingsLoaded && step > CHECKOUT_STEP) {
      setStep(CHECKOUT_STEP)
    }
    // If on package step (4) but package step is disabled, go to checkout (4)
    if (settingsLoaded && step === PACKAGE_STEP && !packageStepEnabled) {
      // step 4 is the checkout step when package is disabled, so this is fine
    }
  }, [settingsLoaded, CHECKOUT_STEP, packageStepEnabled])

  useEffect(() => {
    if (step === 1 && pixelStep1Script && !pixelStep1Fired.current) {
      pixelStep1Fired.current = true
      executePixelScript(pixelStep1Script)
    }
    if (step === CHECKOUT_STEP && pixelStep4Script && !pixelStep4Fired.current) {
      pixelStep4Fired.current = true
      executePixelScript(pixelStep4Script)
    }
    if (step === 1 && tiktokStep1Script && !tiktokStep1Fired.current) {
      tiktokStep1Fired.current = true
      executeTikTokScript(tiktokStep1Script)
    }
    if (step === CHECKOUT_STEP && tiktokStep4Script && !tiktokStep4Fired.current) {
      tiktokStep4Fired.current = true
      executeTikTokScript(tiktokStep4Script)
    }
  }, [step, pixelStep1Script, pixelStep4Script, tiktokStep1Script, tiktokStep4Script])

  // GTM: кастомное событие достижения шага «Выберите стиль» (step=2).
  // Один раз за загрузку страницы; dataLayer очередится, даже если GTM ещё не загрузился.
  useEffect(() => {
    if (step === 2 && !styleStepGtmFired.current) {
      styleStepGtmFired.current = true
      const dataLayer = ((window as unknown as { dataLayer?: Record<string, unknown>[] }).dataLayer ??= [])
      dataLayer.push({ event: 'reach_style_step' })
    }
  }, [step])

  const [emailVerificationId, setEmailVerificationId] = useState<string | null>(null)
  const [otpDigits, setOtpDigits] = useState<string[]>(['', '', '', ''])
  const otpRefs = useRef<Array<HTMLInputElement | null>>([])
  const [otpSending, setOtpSending] = useState(false)
  const [otpVerifying, setOtpVerifying] = useState(false)
  const [otpError, setOtpError] = useState<string | null>(null)
  const [emailVerified, setEmailVerified] = useState(false)
  const [resendCooldownSec, setResendCooldownSec] = useState(0)

  // Server-side draft persistence (best-effort).
  const [draftKey, setDraftKey] = useState<string | null>(null)
  const draftKeyRef = useRef<string | null>(null)
  useEffect(() => {
    draftKeyRef.current = draftKey
  }, [draftKey])

  const DEFAULT_ORDER_INPUT: OrderInput = {
    yourName: undefined,
    recipientName: '',
    occasion: '',
    story: '',
    musicPreferences: {
      genre: 'Pop Ballad',
      mood: '',
      vibe: '',
      tempo: '',
      voiceStyle: 'Surprise me',
      language: 'Russian',
    },
    email: undefined,
    emailVerificationId: undefined,
    extraNotes: '',
  }

  const form = useForm<OrderInput>({
    // NOTE: keep build stable across Zod typings changes between deps.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    resolver: zodResolver(OrderInputSchema as any),
    mode: 'onTouched',
    defaultValues: DEFAULT_ORDER_INPUT,
  })

  const { watch, setValue, register, formState: { errors } } = form
  const recipientName = watch('recipientName')
  const genre = watch('musicPreferences.genre')
  const language = watch('musicPreferences.language')
  const storyText = watch('story') ?? ''
  const email = (watch('email') ?? '').trim()
  const hasEnteredOtp = otpDigits.some((d) => Boolean(d))
  const storyRegister = register('story')

  const didHydrateDraftRef = useRef(false)
  const suppressOtpResetOnceRef = useRef(false)

  function getOrCreateDraftKey() {
    if (draftKeyRef.current) return draftKeyRef.current
    const next = generateDraftKey()
    draftKeyRef.current = next
    setDraftKey(next)
    return next
  }

  const serverDraftSyncTimerRef = useRef<number | null>(null)
  const lastServerDraftSyncJsonRef = useRef<string>('')

  const scheduleServerDraftSync = (values: Partial<OrderInput>) => {
    if (!didHydrateDraftRef.current) return
    if (!hasMeaningfulDraft(values) && step <= 0) return

    const analyticsStep = getAnalyticsStep(step)
    // Package step is transient — skip server draft sync entirely
    if (analyticsStep === null) return

    const key = getOrCreateDraftKey()
    const formValues = getPersistableFormValues(values)
    const payload = {
      draftKey: key,
      step: analyticsStep,
      relationship,
      emailVerificationId,
      formValues,
      themeSlug: themeSlug ?? null,
    }
    const json = JSON.stringify(payload)
    if (json === lastServerDraftSyncJsonRef.current) return

    if (serverDraftSyncTimerRef.current) {
      window.clearTimeout(serverDraftSyncTimerRef.current)
    }
    serverDraftSyncTimerRef.current = window.setTimeout(() => {
      void apiPost('/api/order-drafts/upsert', payload)
        .then(() => {
          lastServerDraftSyncJsonRef.current = json
        })
        .catch(() => {
          // Best-effort only (offline / server error / rate limit)
        })
    }, 1000)
  }

  useEffect(() => {
    return () => {
      if (serverDraftSyncTimerRef.current) window.clearTimeout(serverDraftSyncTimerRef.current)
    }
  }, [])

  const saveDraft = (values: Partial<OrderInput>) => {
    if (!didHydrateDraftRef.current) return
    try {
      const formValues = getPersistableFormValues(values)
      const key = (hasMeaningfulDraft(values) || step > 0) ? getOrCreateDraftKey() : (draftKeyRef.current ?? null)

      const payload: PersistedConfigDraft = {
        v: 1,
        draftKey: key,
        step,
        relationship,
        pickedGender,
        emailVerificationId,
        formValues,
        updatedAt: Date.now(),
      }

      window.localStorage.setItem(CONFIG_DRAFT_STORAGE_KEY, JSON.stringify(payload))
    } catch {
      // Best-effort only (private mode / quota exceeded / JSON issues)
    }

    // Best-effort server persistence.
    scheduleServerDraftSync(values)
  }

  const clearDraft = () => {
    try {
      window.localStorage.removeItem(CONFIG_DRAFT_STORAGE_KEY)
    } catch {
      // ignore
    }
  }

  // Hydrate draft on first mount so user can continue after closing tab/browser.
  useEffect(() => {
    try {
      const raw = window.localStorage.getItem(CONFIG_DRAFT_STORAGE_KEY)
      let parsed: Partial<PersistedConfigDraft> = {}
      
      if (raw) {
        try {
           const p = JSON.parse(raw)
           if (p && p.v === 1) parsed = p
        } catch {
           // ignore invalid json
        }
      }

      const savedStep = Number.isFinite(parsed.step) ? Math.max(0, Math.min(5, parsed.step as number)) : 0

      // Prefer URL step if available and valid
      const urlStepParam = searchParams.get('step')
      let initialStep = savedStep
      if (urlStepParam !== null) {
         const s = Number(urlStepParam)
         if (!isNaN(s) && s >= 0 && s <= 5) {
            initialStep = s
         }
      }

      const nextRelationship = typeof parsed.relationship === 'string' && parsed.relationship ? parsed.relationship : 'Партнёр'
      const nextPickedGender = parsed.pickedGender === 'male' || parsed.pickedGender === 'female' ? parsed.pickedGender : null
      const nextEmailVerificationId = typeof parsed.emailVerificationId === 'string' ? parsed.emailVerificationId : null
      const nextDraftKey = typeof parsed.draftKey === 'string' && parsed.draftKey ? parsed.draftKey : null

      const fv = (parsed.formValues ?? {}) as Partial<OrderInput>
      const merged: OrderInput = {
        ...DEFAULT_ORDER_INPUT,
        ...fv,
        musicPreferences: {
          ...DEFAULT_ORDER_INPUT.musicPreferences,
          ...(fv.musicPreferences ?? {}),
          voiceStyle: fv.musicPreferences?.voiceStyle || DEFAULT_ORDER_INPUT.musicPreferences.voiceStyle,
        },
        emailVerificationId: DEFAULT_ORDER_INPUT.emailVerificationId,
      }

      const relParam = searchParams.get('rel')
      if (relParam) {
        setStep(initialStep)
        setRelationship(relParam)
      } else {
        setStep(initialStep)
        setRelationship(nextRelationship)
        setPickedGender(nextPickedGender)
      }
      setEmailVerificationId(nextEmailVerificationId)
      if (nextDraftKey) {
        draftKeyRef.current = nextDraftKey
        setDraftKey(nextDraftKey)
      } else {
        // Backfill draftKey for older local drafts (v1 without draftKey).
        const created = generateDraftKey()
        draftKeyRef.current = created
        setDraftKey(created)
      }
      suppressOtpResetOnceRef.current = true
      form.reset(merged)
    } catch {
      // ignore and continue with defaults
    } finally {
      didHydrateDraftRef.current = true
      setIsHydrated(true)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // If we have a persisted `emailVerificationId`, re-check it server-side so refresh doesn't force OTP again.
  // Validity is bounded by the OTP record expiry (e.g. 10 minutes).
  useEffect(() => {
    if (!didHydrateDraftRef.current) return
    if (!settingsLoaded || !emailEnabled || !emailOtpEnabled) return
    if (!emailVerificationId) return
    if (!email) return

    let cancelled = false
    type StatusRes = { ok: true; verified: boolean; expired: boolean; email: string; expiresAt: string }

    apiGet<StatusRes>(`/api/email-verification/status/${encodeURIComponent(emailVerificationId)}`)
      .then((res) => {
        if (cancelled) return
        const expected = email.trim().toLowerCase()
        const actual = String(res?.email ?? '').trim().toLowerCase()

        if (res?.ok && res.verified && !res.expired && actual === expected) {
          setEmailVerified(true)
          setValue('emailVerificationId', emailVerificationId)
          setOtpError(null)
          setOtpDigits(['', '', '', ''])
          return
        }

        // Not verified (or expired/mismatch) → keep UI in "unverified" state.
        setEmailVerified(false)
        setValue('emailVerificationId', '')

        // If expired/mismatch/not found, clear verification id so user must request a new OTP.
        if (!res?.ok || res.expired || (actual && actual !== expected)) {
          setEmailVerificationId(null)
          setOtpDigits(['', '', '', ''])
          setOtpError(null)
        }
      })
      .catch((e: unknown) => {
        if (cancelled) return
        // Best-effort only. If the server says it doesn't exist, force re-request.
        const msg = getErrorMessage(e, '')
        if (msg === 'not_found') {
          setEmailVerified(false)
          setValue('emailVerificationId', '')
          setEmailVerificationId(null)
          setOtpDigits(['', '', '', ''])
          setOtpError(null)
        }
      })

    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settingsLoaded, emailEnabled, emailOtpEnabled, emailVerificationId, email])

  // Persist on any form change (best-effort).
  useEffect(() => {
    const sub = form.watch((values) => saveDraft(values as Partial<OrderInput>))
    return () => sub.unsubscribe()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form, step, relationship, pickedGender, emailVerificationId])

  // Persist when non-form state changes (step/relationship/OTP start).
  // Skip until hydration is complete to avoid overwriting URL step=1 with
  // the initial step=0 before the hydration effect has restored the real step.
  useEffect(() => {
    if (!isHydrated) return
    const currentUrlStep = searchParams.get('step')
    if (currentUrlStep !== String(step)) {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev)
        next.set('step', String(step))
        return next
      }, { replace: true })
    }
    saveDraft(form.getValues())
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, relationship, pickedGender, emailVerificationId])

  // Derived state for dynamic titles
  const currentRecipient = recipientName || 'the lucky person'

  // Map "yourName" to the name typed in recipientName (per request)
  useEffect(() => {
    const trimmed = (recipientName ?? '').trim()
    // RHF type for setValue expects string here; we intentionally clear to `undefined` when empty
    setValue('yourName', trimmed || undefined)
  }, [recipientName, setValue])

  const storyLen = storyText.trim().length
  const storyQuality =
    storyLen === 0
      ? { label: 'Продолжайте писать...', pct: 10, color: 'bg-gray-300', text: 'text-gray-500' }
      : storyLen < 80
        ? { label: 'Ещё немного...', pct: 40, color: 'bg-amber-400', text: 'text-amber-700' }
        : storyLen < 160
          ? { label: 'Хорошо!', pct: 75, color: 'bg-green-500', text: 'text-green-600' }
          : { label: 'Отлично!', pct: 100, color: 'bg-green-600', text: 'text-green-700' }

  const minStep = configSteps.step0.enabled ? 0 : 1

  const handleNext = useCallback(async () => {
    if (step === 0) {
      setStep(1)
      window.scrollTo(0, 0)
      return
    }
    let fieldsToValidate: Path<OrderInput>[] = []
    if (step === 1) fieldsToValidate = ['recipientName']
    if (step === 2) fieldsToValidate = ['musicPreferences.genre', 'musicPreferences.voiceStyle']
    if (step === 3) fieldsToValidate = []

    const isStepValid = await form.trigger(fieldsToValidate)
    if (isStepValid) {
      setStep((prev) => Math.min(prev + 1, CHECKOUT_STEP))
      window.scrollTo(0, 0)
    }
  }, [step, form, CHECKOUT_STEP])

  const handleBack = useCallback(() => {
    setStep((prev) => Math.max(prev - 1, minStep))
    window.scrollTo(0, 0)
  }, [minStep])

  // Reset OTP state if email changes (but not if already verified)
  useEffect(() => {
    if (suppressOtpResetOnceRef.current) {
      suppressOtpResetOnceRef.current = false
      return
    }
    // Не сбрасывать если email уже подтверждён
    if (emailVerified) {
      return
    }
    setEmailVerified(false)
    setOtpDigits(['', '', '', ''])
    setEmailVerificationId(null)
    setValue('emailVerificationId', '')
    setOtpError(null)
    setResendCooldownSec(0)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [email])

  useEffect(() => {
    if (resendCooldownSec <= 0) return
    const t = window.setInterval(() => {
      setResendCooldownSec((s) => Math.max(0, s - 1))
    }, 1000)
    return () => window.clearInterval(t)
  }, [resendCooldownSec])

  async function sendEmailOtp() {
    setOtpError(null)
    if (!email) {
      setOtpError('Введите email.')
      return
    }
    // Ensure email is validated even if the field hasn't been touched yet.
    const emailOk = await form.trigger('email')
    if (!emailOk) {
      setOtpError('Некорректный формат email.')
      return
    }
    if (resendCooldownSec > 0) return
    // UX guard: if user already started entering OTP, they should verify instead of requesting again.
    if (emailVerified || hasEnteredOtp) return

    setOtpSending(true)
    try {
      const res = await apiPost<{ verificationId: string; expiresAt: string }>('/api/email-verification/start', { email })
      setEmailVerificationId(res.verificationId)
      setOtpDigits(['', '', '', ''])
      setEmailVerified(false)
      setValue('emailVerificationId', '')
      setResendCooldownSec(60)
      setOtpError(null) // Clear any previous errors on success
      window.setTimeout(() => otpRefs.current?.[0]?.focus?.(), 50)
    } catch (e: unknown) {
      const errorMsg = getErrorMessage(e, 'Не удалось отправить код. Попробуйте снова.')
      console.error('Send OTP error:', e)
      setOtpError(errorMsg)
    } finally {
      setOtpSending(false)
    }
  }

  async function verifyEmailOtp() {
    setOtpError(null)
    if (!emailVerificationId) {
      setOtpError('Сначала нажмите «Отправить код».')
      return
    }
    const code = otpDigits.join('')
    if (code.length !== 4 || otpDigits.some((d) => !d)) {
      setOtpError('Введите 4 цифры кода из письма.')
      return
    }
    setOtpVerifying(true)
    try {
      await apiPost<{ ok: true }>('/api/email-verification/verify', { verificationId: emailVerificationId, code })
      setEmailVerified(true)
      setValue('emailVerificationId', emailVerificationId)
      setOtpError(null) // Clear any previous errors on success
    } catch (e: unknown) {
      setEmailVerified(false)
      setValue('emailVerificationId', '')
      const errorMsg = getErrorMessage(e, 'Неверный или просроченный код.')
      console.error('Verify OTP error:', e)
      setOtpError(errorMsg)
    } finally {
      setOtpVerifying(false)
    }
  }

  function handleOtpChange(idx: number, nextRaw: string) {
    const next = (nextRaw ?? '').replace(/\D/g, '').slice(-1)
    setOtpDigits((prev) => {
      const copy = [...prev]
      copy[idx] = next
      return copy
    })
    if (next && idx < 3) {
      window.setTimeout(() => otpRefs.current?.[idx + 1]?.focus?.(), 0)
    }
  }

  function handleOtpKeyDown(idx: number, e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Backspace') {
      if (otpDigits[idx]) {
        // clear current digit
        e.preventDefault()
        setOtpDigits((prev) => {
          const copy = [...prev]
          copy[idx] = ''
          return copy
        })
        return
      }
      if (idx > 0) {
        e.preventDefault()
        otpRefs.current?.[idx - 1]?.focus?.()
      }
    }
    if (e.key === 'ArrowLeft' && idx > 0) otpRefs.current?.[idx - 1]?.focus?.()
    if (e.key === 'ArrowRight' && idx < 3) otpRefs.current?.[idx + 1]?.focus?.()
  }

  function handleOtpPaste(e: React.ClipboardEvent<HTMLInputElement>) {
    const text = e.clipboardData.getData('text') ?? ''
    const digits = text.replace(/\D/g, '').slice(0, 4).split('')
    if (digits.length === 0) return
    e.preventDefault()
    setOtpDigits([digits[0] ?? '', digits[1] ?? '', digits[2] ?? '', digits[3] ?? ''])
    const nextFocus = Math.min(digits.length, 4) - 1
    window.setTimeout(() => otpRefs.current?.[Math.max(0, nextFocus)]?.focus?.(), 0)
  }

  function appendStoryPrompt(prompt: string) {
    const current = storyText ?? ''
    const currentHasMeaningfulText = current.trim().length > 0
    const separator = currentHasMeaningfulText && !current.endsWith('\n') ? '\n' : ''
    const next = `${currentHasMeaningfulText ? current : ''}${separator}${prompt}:\n`

    setValue('story', next, { shouldDirty: true, shouldTouch: true, shouldValidate: true })

    window.setTimeout(() => {
      const el = storyTextareaRef.current
      if (!el) return
      el.focus()
      try {
        const end = el.value.length
        el.setSelectionRange(end, end)
      } catch {
        // ignore (some browsers can throw if textarea isn't focusable yet)
      }
    }, 0)
  }

  // If bundle package is selected, include all linked upsell prices in displayed amount
  const packageUpsellAmount = selectedPackageUpsellIds.length > 0
    ? linkedUpsellItems.filter((it) => selectedPackageUpsellIds.includes(it.id)).reduce((sum, it) => sum + it.price, 0)
    : 0
  const effectivePaymentAmount = paymentAmount + packageUpsellAmount

  const discountAmountValue = appliedDiscount
    ? appliedDiscount.type === 'percentage'
      ? Math.floor(effectivePaymentAmount * (appliedDiscount.percentageValue ?? 0) / 100)
      : (appliedDiscount.amount ?? 0)
    : 0
  const discountedPaymentAmount = appliedDiscount
    ? Math.max(0, effectivePaymentAmount - discountAmountValue)
    : effectivePaymentAmount

  async function validateDiscountCode() {
    const code = discountCodeInput.trim()
    if (!code) return
    setDiscountValidating(true)
    setDiscountError(null)
    try {
      const res = await apiPost<{ valid: boolean; discountType?: string; discountAmount?: number; percentageValue?: number; error?: string }>('/api/public/discount/validate', {
        code,
        templateSlug: themeSlug ?? null,
        phoneNumber: null,
      })
      if (res.valid) {
        if (res.discountType === 'percentage' && typeof res.percentageValue === 'number') {
          setAppliedDiscount({ code: code.toUpperCase(), type: 'percentage', percentageValue: res.percentageValue })
        } else if (typeof res.discountAmount === 'number') {
          setAppliedDiscount({ code: code.toUpperCase(), type: 'fixed', amount: res.discountAmount })
        }
        setDiscountError(null)
      } else {
        setDiscountError(res.error ?? 'Промокод недействителен.')
        setAppliedDiscount(null)
      }
    } catch (e) {
      setDiscountError(e instanceof Error ? e.message : 'Не удалось проверить промокод.')
      setAppliedDiscount(null)
    } finally {
      setDiscountValidating(false)
    }
  }

  const themeColors = useMemo(() => {
    const cfg = publicSiteConfig && typeof publicSiteConfig === 'object' ? (publicSiteConfig as Loose) : {}
    const c = cfg?.colors && typeof cfg.colors === 'object' ? cfg.colors : {}
    return {
      accentColor: typeof c.accentColor === 'string' ? c.accentColor : '#D4AF37',
      buttonColor: typeof c.buttonColor === 'string' ? c.buttonColor : '',
    }
  }, [publicSiteConfig])

  const onSubmit = async (data: OrderInput) => {
    // Validate email if enabled
    if (!manualConfirmationEnabled && emailEnabled) {
      if (!data.email) {
        form.setError('email', { type: 'manual', message: 'Укажите email' })
        emailSectionRef.current?.scrollIntoView({ behavior: 'smooth', block: 'center' })
        return
      }
      if (emailOtpEnabled && (!emailVerified || !data.emailVerificationId)) {
        setError('Сначала подтвердите email, затем переходите к оплате.')
        emailSectionRef.current?.scrollIntoView({ behavior: 'smooth', block: 'center' })
        return
      }
    }

    const basePayload: Record<string, unknown> = { ...data }
    if (manualConfirmationEnabled || !emailEnabled) {
      delete basePayload.email
      delete basePayload.emailVerificationId
    } else if (!emailOtpEnabled) {
      delete basePayload.emailVerificationId
    }
    if (agreementEnabled) basePayload.agreementAccepted = true

    // GA4 client id from the _ga cookie (GA1.1.<client_id>) — lets the server
    // attribute its server-side purchase event to this browser session.
    const gaMatch = typeof document !== 'undefined' ? document.cookie.match(/_ga=GA\d\.\d\.(\d+\.\d+)/) : null
    const gaClientId = gaMatch ? gaMatch[1] : null

    // ClientID Метрики из куки _ym_uid — для серверных офлайн-конверсий.
    const ymMatch = typeof document !== 'undefined' ? document.cookie.match(/_ym_uid=(\d+)/) : null
    const ymClientId = ymMatch ? ymMatch[1] : null

    // Рекламные метки посадочной страницы — чтобы источник заказа был виден
    // в /admin/orders, а не только в логах nginx.
    const attribution = getAttribution()

    // Отношение и пол получателя нужны генерации текста: без них модель угадывает
    // род по имени и путает «он»/«она». Раньше выбор «Кому дарим?» доезжал до
    // сервера только как свободная строка внутри extraNotes и в промпт не попадал.
    const recipientGender = genderFromRelationship(relationship) ?? pickedGender

    const formPayload = {
      ...basePayload,
      relationship,
      ...(recipientGender ? { recipientGender } : {}),
      themeSlug: themeSlug ?? null,
      ...(gaClientId ? { gaClientId } : {}),
      ...(ymClientId ? { ymClientId } : {}),
      ...(attribution ? { attribution } : {}),
      ...(appliedDiscount ? { discountCode: appliedDiscount.code } : {}),
      ...(draftKeyRef.current ? { draftKey: draftKeyRef.current } : {}),
    }

    // Determine upsell mode from config
    const upsellCfg = publicSiteConfig && typeof publicSiteConfig === 'object' ? (publicSiteConfig as Loose)?.upsell : null
    const upsellMode = upsellCfg?.upsellMode === 'post-payment' ? 'post-payment' : 'pre-payment'

    // Pre-selected upsells from the package selection step
    const preSelectedUpsellIds = selectedPackageUpsellIds

    if (upsellConfig && upsellMode !== 'post-payment') {
      clearDraft()
      const upsellPath = themeSlug ? `/${themeSlug}/upsell` : '/upsell'
      navigate(upsellPath, {
        state: {
          formPayload,
          upsellConfig,
          themeColors,
          wishlistPixelId,
          manualConfirmationEnabled,
          preSelectedUpsellIds,
        },
      })
      return
    }

    // Include package upsell in direct submission (no upsell route or post-payment mode)
    const finalPayload = preSelectedUpsellIds.length > 0
      ? { ...formPayload, upsells: preSelectedUpsellIds.map((id) => ({ id })) }
      : formPayload

    setLoading(true)
    setError(null)
    let navigatingAway = false
    try {
      // Admin mode: internal staff bypass payment entirely. The endpoint creates a
      // paid order and triggers generation. Ведём на /thanks, как реальный
      // return_url ЮKassa — тестовый путь повторяет боевой, и при включённом
      // тоггле «Тестовые в аналитику» там отбивается браузерный трекинг.
      if (adminMode) {
        const res = await apiPost<{ orderId: string }>('/api/admin/orders/draft', finalPayload)
        clearDraft()
        navigate(`/thanks?orderId=${encodeURIComponent(res.orderId)}`)
        return
      }

      // Subscriber mode: песня входит в подписку, оплата не нужна. Заказ сразу
      // paid, генерация стартует на сервере — ведём на страницу песни.
      if (subscriberMode) {
        try {
          const res = await apiPost<{ orderId: string }>('/api/subscription/orders/draft', finalPayload)
          clearDraft()
          navigate(`/order/${encodeURIComponent(res.orderId)}`)
        } catch (err) {
          const msg = err instanceof Error ? err.message : ''
          if (msg === 'daily_limit_reached') {
            setError('Суточный лимит песен по подписке исчерпан — возвращайтесь завтра.')
          } else if (msg === 'unauthorized') {
            navigate('/account')
          } else {
            setError('Не получилось создать песню. Попробуйте ещё раз.')
          }
        }
        return
      }

      const res = await apiPost<{ orderId: string; xenditInvoiceUrl?: string; paymentStatus?: string }>('/api/orders/draft', finalPayload)
      clearDraft()

      trackPixelEvent('Lead', { content_name: 'AI Song', currency: 'RUB', value: 0 }, `order_created:${res.orderId}`)
      trackTikTokEvent('SubmitForm', { content_name: 'AI Song', currency: 'RUB', value: 0 })

      if (wishlistPixelId) {
        ensureMetaPixelLoaded(wishlistPixelId)
        trackWishlist(wishlistPixelId, { content_name: 'Персональная песня' })
      }

      if (manualConfirmationEnabled) {
        const adminNumber = '62895370231680'
        const recipient = String(data.recipientName ?? '').trim()
        const extraNotes = String(data.extraNotes ?? '').trim()

        const message = [
          'Здравствуйте!',
          '',
          'Хочу подтвердить заказ персональной песни.',
          '',
          `Номер заказа: ${res.orderId}`,
          `Имя адресата: ${recipient || '—'}`,
          ...(extraNotes ? ['', `Комментарий: ${extraNotes}`] : []),
          '',
          'Просьба обработать заказ. Спасибо!',
        ].join('\n')

        const url = `https://wa.me/${adminNumber}?text=${encodeURIComponent(message)}`
        navigatingAway = true
        window.location.assign(url)
        return
      }

      if (res.xenditInvoiceUrl) {
        navigatingAway = true
        setNavigatingToPayment(true)
        window.location.href = res.xenditInvoiceUrl
        return
      }

      if (res.paymentStatus === 'pending') {
        const checkoutUrl = `/checkout?orderId=${encodeURIComponent(res.orderId)}${appliedDiscount ? `&discountCode=${encodeURIComponent(appliedDiscount.code)}` : ''}`
        navigate(checkoutUrl)
        return
      }

      navigate(`/order/${encodeURIComponent(res.orderId)}`)
    } catch (err: unknown) {
      const msg = getErrorMessage(err, 'Что-то пошло не так. Попробуйте ещё раз.')
      setError(msg)
    } finally {
      if (!navigatingAway) setLoading(false)
    }
  }

  const onInvalid = (_errs: FieldErrors<OrderInput>) => {
    // Make it visible even if the invalid field is on a previous step.
    setError('Заполните обязательные поля на предыдущих шагах, затем продолжите.')
  }

  // When admin enables manual confirmation, the public flow must not be blocked by hidden email/OTP fields.
  useEffect(() => {
    if (!manualConfirmationEnabled) return
    form.clearErrors(['email', 'emailVerificationId'])
    setValue('email', undefined, { shouldDirty: false, shouldTouch: false, shouldValidate: false })
    setValue('emailVerificationId', undefined, { shouldDirty: false, shouldTouch: false, shouldValidate: false })
    setEmailVerificationId(null)
    setOtpDigits(['', '', '', ''])
    setOtpError(null)
    setEmailVerified(false)
  }, [manualConfirmationEnabled, form, setValue, step])

  const rawThemeColors = (publicSiteConfig as Loose)?.colors
  const themeStyle = {
    '--theme-accent': rawThemeColors?.accentColor || '#D4AF37',
    '--theme-button': rawThemeColors?.buttonColor || rawThemeColors?.accentColor || '#D4AF37',
    '--theme-accent-soft': rawThemeColors?.bgColor1 || '#FDFBF7',
    '--theme-bg': rawThemeColors?.bgColor2 || '#FFFFFF',
  } as React.CSSProperties


  if (!isHydrated || publicSiteConfig === undefined || navigatingToPayment) {
    return (
      <div className="min-h-screen flex items-center justify-center" style={publicSiteConfig !== undefined ? themeStyle : undefined}>
        <Loader2 className="h-8 w-8 animate-spin text-gray-400" />
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-[var(--theme-accent-soft)] font-sans text-gray-900 pb-32" style={themeStyle}>
      {checkoutExtraData.promoBanner.enabled && (
        <div className={step === CHECKOUT_STEP ? 'hidden sm:block' : undefined}>
        <CountdownTimer
          promoBadgeText={checkoutExtraData.promoBanner.promoBadgeText || 'Акция'}
          countdownLabel={checkoutExtraData.promoBanner.countdownLabel}
          countdownTargetDate={checkoutExtraData.promoBanner.countdownTargetDate}
          evergreenEnabled={checkoutExtraData.promoBanner.evergreenEnabled}
          evergreenCycleHours={checkoutExtraData.promoBanner.evergreenCycleHours}
        />
        </div>
      )}

      {/* Header */}
      <header className={`${step === CHECKOUT_STEP ? 'hidden sm:block sm:sticky' : 'sticky'} top-0 z-50 border-b border-[var(--theme-accent-soft)] bg-white/95 backdrop-blur-sm shadow-sm`}>
        <div className="mx-auto flex max-w-4xl items-center justify-between px-4 py-2">
          <span />
          {funnelPriceVisibility.funnelHeader && (
            <div className="text-right flex items-center gap-1.5">
               <span className="text-[10px] sm:text-xs text-gray-400 line-through">{fmtCurrency(originalAmount)}</span>
               <span className="text-sm sm:text-lg font-bold text-[var(--theme-accent-text)]">{fmtCurrency(effectivePaymentAmount)}</span>
            </div>
          )}
        </div>
        {step > 0 && step < CHECKOUT_STEP && (
          <div className="px-4 pb-2 pt-1 max-w-4xl mx-auto w-full">
            <div className="w-full h-1.5 bg-gray-200 rounded-full overflow-hidden">
              <div className="h-full bg-[var(--theme-accent)] rounded-full transition-all duration-500" style={{ width: `${(step / TOTAL_STEPS) * 100}%` }} />
            </div>
            <div className="flex items-center justify-between mt-1">
              <span className="text-[11px] font-semibold text-gray-700">Шаг {step} из {TOTAL_STEPS}</span>
              <span className="text-[11px] text-gray-400 flex items-center gap-1"><Clock className="h-3 w-3" /> ~2 мин</span>
            </div>
          </div>
        )}
      </header>

      <main className="mx-auto max-w-4xl w-full px-4 pt-0 pb-4 sm:py-8 lg:py-12">
        <form onSubmit={form.handleSubmit(onSubmit, onInvalid)}>
          
          {/* STEP 0: ANNOUNCEMENT - halaman pengumuman */}
          {step === 0 && configSteps.step0.enabled && (
            <div className="animate-in fade-in slide-in-from-bottom-4 duration-500 pb-4 -mx-4 sm:mx-0">
              <div className="bg-[var(--theme-button)] px-4 py-3 flex items-center justify-center gap-2 text-white">
                <Megaphone className="h-4 w-4 shrink-0" />
                <span className="text-xs sm:text-sm font-bold uppercase tracking-wider">{configSteps.step0.bannerHeadline}</span>
              </div>

              <div className="bg-white px-4 sm:px-8 py-6 sm:py-8 text-center space-y-4 sm:space-y-6">
                <h2 className="text-sm sm:text-xl md:text-2xl font-bold text-gray-900 leading-snug mx-auto max-w-lg" dangerouslySetInnerHTML={{ __html: configSteps.step0.mainHeadline }} />

                <div className="relative mx-auto rounded-xl overflow-hidden bg-gray-100 shadow-md ring-1 ring-gray-200 w-[110px] sm:w-[180px] md:w-[220px] aspect-[9/16]">
                   {heroVideoUrl ? (
                     <video 
                       src={heroVideoUrl} 
                       autoPlay 
                       loop 
                       muted 
                       playsInline
                       className="w-full h-full object-cover"
                     />
                   ) : (
                     <div className="flex items-center justify-center h-full text-gray-400 text-xs italic">Превью видео</div>
                   )}
                </div>

                <div className="mx-auto max-w-md w-full">
                  <div className="bg-green-50 border border-green-200 rounded-xl p-3 sm:p-4 text-xs sm:text-sm text-green-800 text-center">
                    <p className="font-bold mb-1 flex items-center justify-center gap-2 text-base sm:text-lg"><span className="text-xl">🤑</span> {configSteps.step0.guaranteeTitle}</p>
                    <p className="leading-relaxed opacity-90" dangerouslySetInnerHTML={{ __html: configSteps.step0.guaranteeText }} />
                  </div>
                  <p className="text-[10px] sm:text-xs text-gray-400 italic mt-2 text-center">*Действуют условия акции</p>
                </div>
              </div>

              <div className="bg-gradient-to-b from-[var(--theme-accent-soft)] to-white px-4 py-6 sm:py-10 border-t border-[var(--theme-accent-soft)]">
                <h3 className="text-[10px] sm:text-xs font-bold text-[var(--theme-accent-text)] mb-4 sm:mb-8 text-center uppercase tracking-widest">{configSteps.step0.howItWorksTitle}</h3>
                
                <div className={`grid ${configSteps.step0.howItWorksSteps.length <= 2 ? 'grid-cols-2' : configSteps.step0.howItWorksSteps.length >= 4 ? 'grid-cols-4' : 'grid-cols-3'} gap-4 sm:gap-8 max-w-lg mx-auto`}>
                  {configSteps.step0.howItWorksSteps.map((s, i) => (
                    <div key={i} className="flex flex-col items-center text-center">
                      <div className="w-6 h-6 sm:w-8 sm:h-8 rounded-full bg-[var(--theme-accent)] text-white flex items-center justify-center text-xs sm:text-sm font-bold mb-2 shadow-sm">{i + 1}</div>
                      <p className="font-bold text-[10px] sm:text-sm text-gray-800 leading-tight">{s.title}</p>
                      <p className="text-[9px] sm:text-xs text-gray-500 leading-none mt-1">{s.subtitle}</p>
                    </div>
                  ))}
                </div>
              </div>

              <p className="text-center text-[10px] sm:text-sm text-gray-400 mt-4 px-4 animate-bounce">{configSteps.step0.bottomCtaText}</p>
            </div>
          )}

          {/* STEP 1: WHO IS IT FOR? */}
          {step === 1 && (
            <div className="space-y-4 animate-in fade-in slide-in-from-bottom-4 duration-500 pt-2">
              <div className="text-center space-y-1">
                <h1 className="text-xl font-bold text-gray-900 flex items-center justify-center gap-2">
                  <Heart className="h-5 w-5 text-[var(--theme-accent-text)] fill-[var(--theme-accent-soft)]" />
                  {configSteps.step1.headline}
                </h1>
                <p className="text-xs text-gray-500">{configSteps.step1.subtitle}</p>
              </div>

              <div className="flex flex-wrap gap-2">
                {[...configSteps.step1.relationshipChips, { label: 'Другое', icon: '', value: 'Другое' }].map((chip) => (
                  <SelectionChip
                    key={chip.value}
                    label={chip.label}
                    icon={chip.icon}
                    selected={relationship === chip.value}
                    onClick={() => {
                      setRelationship(chip.value)
                      setPickedGender(null)
                      setValue('extraNotes', `Отношение к адресату: ${chip.value}`)
                      if (chip.value !== 'Другое') setValue('occasion', '')
                    }}
                  />
                ))}
              </div>

              {relationship === 'Другое' && (
                <div className="space-y-2 animate-in fade-in slide-in-from-top-2 duration-300">
                  <label className="text-[10px] font-bold uppercase tracking-wider text-gray-500">{configSteps.step1.occasionFieldLabel}</label>
                  <Input 
                    {...register('occasion')} 
                    placeholder={configSteps.step1.occasionFieldPlaceholder}
                    className="h-11 rounded-xl border-gray-300 text-base shadow-sm bg-white focus-visible:ring-[var(--theme-accent)]"
                  />
                </div>
              )}

              <div className="space-y-2">
                <label className="text-[10px] font-bold uppercase tracking-wider text-gray-500">{configSteps.step1.nameFieldLabel}</label>
                <Input 
                  {...register('recipientName')} 
                  placeholder={configSteps.step1.nameFieldPlaceholder}
                  className="h-11 rounded-xl border-gray-300 text-base shadow-sm focus-visible:ring-[var(--theme-accent)]"
                />
                {errors.recipientName && <p className="text-xs text-[var(--theme-accent-text)]">{errors.recipientName.message}</p>}
                <div className="flex items-center gap-1.5 text-[10px] text-amber-600 bg-amber-50 p-2 rounded-lg border border-amber-100">
                  <Sparkles className="h-3 w-3 shrink-0" />
                  Имя попадёт в текст песни!
                </div>
              </div>

              {/* Пол получателя нужен, чтобы в песне не путались «он» и «она».
                  Для «Мужа» или «Мамы» он уже известен из выбора выше, поэтому
                  спрашиваем только там, где отношение пола не задаёт. */}
              {genderFromRelationship(relationship) === null && (
                <div className="space-y-2 animate-in fade-in slide-in-from-top-2 duration-300">
                  <label className="text-[10px] font-bold uppercase tracking-wider text-gray-500">Кому поём — ему или ей?</label>
                  <div className="flex gap-2">
                    {([
                      { value: 'male', label: 'Ему' },
                      { value: 'female', label: 'Ей' },
                    ] as { value: RecipientGender; label: string }[]).map((option) => (
                      <SelectionChip
                        key={option.value}
                        label={option.label}
                        selected={pickedGender === option.value}
                        onClick={() => setPickedGender(option.value)}
                      />
                    ))}
                  </div>
                </div>
              )}

              {configSteps.step1.reactionVideosEnabled && configSteps.step1.reactionVideosThumbnails.length > 0 && configSteps.step1.reactionVideosVideoUrl && (
                <ReactionVideosSection
                  headline={configSteps.step1.reactionVideosHeadline}
                  thumbnails={configSteps.step1.reactionVideosThumbnails}
                  videoUrl={configSteps.step1.reactionVideosVideoUrl}
                />
              )}

              {configSteps.step1.socialProofText && (
                <div className="text-center text-[10px] text-gray-400 mt-4">
                  {configSteps.step1.socialProofText}
                </div>
              )}
            </div>
          )}

          {/* STEP 2: VIBE */}
          {step === 2 && (
            <div className="space-y-4 animate-in fade-in slide-in-from-bottom-4 duration-500 pt-2">
              <div className="text-center space-y-1">
                <h1 className="text-xl font-bold text-gray-900 flex items-center justify-center gap-2">
                  <Music className="h-5 w-5 text-[var(--theme-accent-text)]" />
                  {configSteps.step2.headline}
                </h1>
                <p className="text-xs text-gray-500">{configSteps.step2.subtitle}</p>
              </div>

              <div className="flex flex-wrap gap-2">
                {configSteps.step2.vibeChips.map((g) => (
                  <VibeCard
                    key={g.id}
                    label={g.label}
                    description={g.desc}
                    icon={g.icon}
                    badge={g.badge}
                    selected={genre === g.id}
                    onClick={() => setValue('musicPreferences.genre', g.id)}
                  />
                ))}
              </div>

              <div className="rounded-xl bg-white p-3 shadow-sm border border-gray-100 text-center space-y-2">
                <label className="text-[10px] font-bold uppercase tracking-wider text-gray-500">{configSteps.step2.voiceLabel}</label>
                <div className="flex justify-center gap-4">
                  {configSteps.step2.voiceOptions.map((v) => (
                    <label key={v.value} className="flex items-center gap-1.5 cursor-pointer bg-gray-50 px-3 py-2 rounded-lg border border-gray-100 hover:bg-gray-100 transition-colors">
                      <input 
                        type="radio" 
                        value={v.value} 
                        {...register('musicPreferences.voiceStyle')}
                        className="text-[var(--theme-accent-text)] focus:ring-[var(--theme-accent)] w-3 h-3" 
                      />
                      <span className="text-xs font-medium">{v.label}</span>
                    </label>
                  ))}
                </div>
              </div>

              <div className="rounded-xl bg-white p-3 shadow-sm border border-gray-100 text-center space-y-2">
                <label className="text-[10px] font-bold uppercase tracking-wider text-gray-500">{configSteps.step2.languageLabel}</label>
                <select
                  className="w-full rounded-xl border border-gray-200 bg-white px-3 py-2 text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-[var(--theme-accent)]"
                  value={language || configSteps.step2.languageOptions[0]?.value || 'Russian'}
                  onChange={(e) => setValue('musicPreferences.language', e.target.value)}
                >
                  {configSteps.step2.languageOptions.map((opt) => (
                    <option key={opt.value} value={opt.value}>{opt.label}</option>
                  ))}
                </select>
              </div>
            </div>
          )}

          {/* STEP 3: STORY */}
          {step === 3 && (
            <div className="space-y-4 animate-in fade-in slide-in-from-bottom-4 duration-500 pt-2">
              <div className="text-center space-y-1">
                <h1 className="text-xl font-bold text-gray-900 flex items-center justify-center gap-2">
                  <PenLine className="h-5 w-5 text-[var(--theme-accent-text)]" />
                  {configSteps.step3.headline}
                </h1>
                <p className="text-xs text-gray-500" dangerouslySetInnerHTML={{ __html: configSteps.step3.subtitle }} />
              </div>

              <div className="space-y-2 text-sm text-gray-600">
                {configSteps.step3.tipBullets.map((bullet, i) => (
                  <div key={i}>
                    <span>{bullet}</span>
                  </div>
                ))}
              </div>

              <div className="space-y-2">
                <div className="flex items-center gap-1 text-[10px] font-bold text-gray-400 uppercase tracking-wider">
                  <Sparkles className="h-3 w-3" /> Идеи для темы:
                </div>
                <div className="flex flex-wrap gap-2">
                  {configSteps.step3.storyPrompts.map((prompt) => (
                    <PromptChip 
                      key={prompt.label} 
                      label={prompt.label} 
                      icon={prompt.icon}
                      onClick={() => appendStoryPrompt(prompt.label)}
                    />
                  ))}
                </div>
              </div>

              <div className="space-y-2">
                <Textarea 
                  {...storyRegister}
                  ref={(el) => {
                    storyRegister.ref(el)
                    storyTextareaRef.current = el
                  }}
                  rows={5} 
                  placeholder={configSteps.step3.textareaPlaceholder}
                  maxLength={4000}
                  className="rounded-xl border-gray-300 text-base shadow-sm focus-visible:ring-[var(--theme-accent)] resize-none p-4"
                />
                <div className="flex justify-between text-xs">
                  <div className={["flex items-center gap-2 font-medium", storyQuality.text].join(' ')}>
                    <div className="h-1.5 w-16 rounded-full bg-gray-200 overflow-hidden">
                      <div
                        className={["h-full transition-all duration-300", storyQuality.color].join(' ')}
                        style={{ width: `${storyQuality.pct}%` }}
                      />
                    </div>
                    {storyQuality.label}
                  </div>
                  <span className="text-gray-400">{storyText.length} / 4000</span>
                </div>
              </div>
            </div>
          )}

          {/* PACKAGE SELECTION STEP (optional, between story and checkout) */}
          {step === PACKAGE_STEP && packageStepEnabled && (() => {
            const baseCard = (
              <button
                key="base"
                type="button"
                onClick={() => { setSelectedPackageUpsellIds([]); setStep((prev) => Math.min(prev + 1, CHECKOUT_STEP)); window.scrollTo(0, 0); }}
                className="text-left w-full rounded-2xl border-2 border-gray-200 bg-white p-5 shadow-sm hover:border-gray-400 hover:shadow-md transition-all active:scale-[0.98]"
              >
                <h3 className="text-lg font-bold text-gray-900">{configSteps.packageStep.basePackage.name}</h3>
                <p className="text-sm text-gray-500 mt-1">{configSteps.packageStep.basePackage.description}</p>
                {configSteps.packageStep.basePackage.bullets.length > 0 && (
                  <ul className="mt-3 space-y-1.5">
                    {configSteps.packageStep.basePackage.bullets.map((b, i) => (
                      <li key={i} className="flex items-start gap-2 text-sm text-gray-600">
                        <Check className="h-4 w-4 text-gray-400 mt-0.5 shrink-0" />
                        {b.text}
                      </li>
                    ))}
                  </ul>
                )}
                <div className="mt-4 flex items-baseline gap-2">
                  <span className="text-2xl font-bold text-gray-900">{fmtCurrency(paymentAmount)}</span>
                </div>
                <div className="mt-3 w-full rounded-xl bg-gray-100 text-center py-3 text-sm font-bold text-gray-700">
                  {configSteps.packageStep.basePackage.ctaText}
                </div>
              </button>
            )
            const bundleUpsellTotal = linkedUpsellItems.reduce((sum, it) => sum + it.price, 0)
            const bundlePrice = paymentAmount + bundleUpsellTotal
            const bundleCard = (
              <button
                key="bundle"
                type="button"
                onClick={() => { setSelectedPackageUpsellIds(linkedUpsellItems.map((it) => it.id)); setStep((prev) => Math.min(prev + 1, CHECKOUT_STEP)); window.scrollTo(0, 0); }}
                className="text-left w-full rounded-2xl border-2 border-[var(--theme-accent)] bg-white p-5 shadow-md hover:shadow-lg transition-all active:scale-[0.98] relative"
              >
                <div className="absolute -top-3 left-4 bg-[var(--theme-accent)] text-white text-[10px] font-bold px-3 py-0.5 rounded-full uppercase tracking-wider">
                  Best Value
                </div>
                <h3 className="text-lg font-bold text-gray-900">{configSteps.packageStep.bundlePackage.name}</h3>
                <p className="text-sm text-gray-500 mt-1">{configSteps.packageStep.bundlePackage.description}</p>
                {configSteps.packageStep.bundlePackage.bullets.length > 0 && (
                  <ul className="mt-3 space-y-1.5">
                    {configSteps.packageStep.bundlePackage.bullets.map((b, i) => (
                      <li key={i} className="flex items-start gap-2 text-sm text-gray-600">
                        <Check className="h-4 w-4 text-[var(--theme-accent-text)] mt-0.5 shrink-0" />
                        {b.text}
                      </li>
                    ))}
                  </ul>
                )}
                <div className="mt-4 flex items-baseline gap-2">
                  <span className="text-2xl font-bold text-[var(--theme-accent-text)]">{fmtCurrency(bundlePrice)}</span>
                </div>
                <div className="mt-3 w-full rounded-xl bg-[var(--theme-button)] text-center py-3 text-sm font-bold text-white">
                  {configSteps.packageStep.bundlePackage.ctaText}
                </div>
              </button>
            )
            const cards = configSteps.packageStep.displayOrder === 'base-first' ? [baseCard, bundleCard] : [bundleCard, baseCard]
            return (
              <div className="space-y-4 animate-in fade-in slide-in-from-bottom-4 duration-500 pt-2">
                <div className="text-center space-y-1">
                  <h1 className="text-xl font-bold text-gray-900 flex items-center justify-center gap-2">
                    <Sparkles className="h-5 w-5 text-[var(--theme-accent-text)]" />
                    {configSteps.packageStep.headline}
                  </h1>
                  <p className="text-xs text-gray-500">{configSteps.packageStep.subtitle}</p>
                </div>
                <div className="flex flex-col gap-4 pt-2">
                  {cards}
                </div>
                <div className="text-center pt-2">
                  <button
                    type="button"
                    onClick={handleBack}
                    className="text-xs text-gray-400 hover:text-gray-600 transition-colors inline-flex items-center gap-1"
                  >
                    <ChevronLeft className="h-3 w-3" /> Назад
                  </button>
                </div>
              </div>
            )
          })()}

          {/* CHECKOUT STEP (was Step 4, now dynamic) */}
          {step === CHECKOUT_STEP && (
            <div className="space-y-5 animate-in fade-in slide-in-from-bottom-4 duration-500 pt-2 pb-4">

              {/* 1. Headline + subtitle + delivery badge */}
              <div className="text-center space-y-2">
                <PartyPopper className="mx-auto h-6 w-6 text-yellow-500 hidden sm:block" />
                <h1 className="text-2xl font-serif font-bold text-gray-900">{configSteps.step4.headline}</h1>
                <p className="text-sm text-gray-500">
                  {manualConfirmationEnabled
                    ? configSteps.step4.manualSubtitle
                    : configSteps.step4.subtitleTemplate.replace('{recipient}', currentRecipient)}
                </p>
                <div className="inline-flex items-center gap-1.5 bg-green-50 text-green-700 text-xs font-semibold px-3 py-1 rounded-full border border-green-200">
                  <Zap className="h-3 w-3" />
                  {deliveryEta.label}
                </div>
              </div>

              {/* 2. Contact inputs (Email + OTP) */}
              <div className="space-y-4">
                {!manualConfirmationEnabled && emailEnabled ? (
                  <>
                    <div className="space-y-1">
                      <div ref={emailSectionRef} />
                      <label className="text-[10px] font-bold uppercase tracking-wider text-gray-500">{configSteps.step4.emailLabel}</label>
                      <div className="relative">
                        <div className="pointer-events-none absolute inset-y-0 left-3 flex items-center text-gray-400">
                          <Mail className="h-4 w-4" />
                        </div>
                        <Input
                          {...register('email', {
                            onChange: () => { form.clearErrors('email'); setError(null) },
                          })}
                          placeholder={configSteps.step4.emailPlaceholder}
                          type="email"
                          disabled={emailVerified}
                          className={`h-12 rounded-xl shadow-sm focus-visible:ring-[var(--theme-accent)] pl-10 ${errors.email ? 'border-[var(--theme-accent)] border-2' : 'border-gray-300'} ${emailVerified ? 'bg-gray-50 cursor-not-allowed' : ''}`}
                        />
                      </div>
                      {errors.email && <p className="text-xs text-[var(--theme-accent-text)]">{errors.email.message}</p>}
                    </div>

                    {emailOtpEnabled ? (
                      <div className="rounded-xl border border-gray-200 bg-white p-3 shadow-sm space-y-3">
                        <div className="flex items-center justify-between gap-3">
                          <div className="text-xs font-bold text-gray-900 uppercase tracking-wider">Подтверждение email</div>
                          {emailVerified ? (
                            <div className="inline-flex items-center gap-1 text-[10px] font-bold text-green-700 bg-green-50 border border-green-100 px-2 py-0.5 rounded-full">
                              <Check className="h-3 w-3" /> OK
                            </div>
                          ) : null}
                        </div>

                        {emailVerified ? (
                          <div className="text-xs text-green-800 flex items-center gap-2">
                            <div className="h-6 w-6 rounded-full bg-green-100 flex items-center justify-center shrink-0">
                              <Check className="h-3.5 w-3.5 text-green-600" />
                            </div>
                            <div>
                              Адрес <span className="font-bold">{email}</span> подтверждён.
                            </div>
                          </div>
                        ) : (
                          <>
                            <div className="flex gap-2">
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="h-10 rounded-lg border-gray-200 w-full text-xs"
                                onClick={() => void sendEmailOtp()}
                                disabled={
                                  otpSending ||
                                  otpVerifying ||
                                  !email ||
                                  Boolean(errors.email) ||
                                  resendCooldownSec > 0 ||
                                  hasEnteredOtp
                                }
                              >
                                {otpSending ? (
                                  <span className="flex items-center gap-2">
                                    <Loader2 className="h-3 w-3 animate-spin" /> Отправка…
                                  </span>
                                ) : hasEnteredOtp ? (
                                  'Проверьте письмо с кодом'
                                ) : resendCooldownSec > 0 ? (
                                  `Отправить снова (${resendCooldownSec} с)`
                                ) : (
                                  'Отправить код'
                                )}
                              </Button>
                            </div>

                            {emailVerificationId && !emailVerified ? (
                              <div className="space-y-2 pt-1">
                                <div className="flex gap-2 justify-center">
                                  {[0, 1, 2, 3].map((i) => (
                                    <Input
                                      key={i}
                                      ref={(el) => {
                                        otpRefs.current[i] = el
                                      }}
                                      value={otpDigits[i] ?? ''}
                                      onChange={(e) => handleOtpChange(i, e.target.value)}
                                      onKeyDown={(e) => handleOtpKeyDown(i, e)}
                                      onPaste={i === 0 ? handleOtpPaste : undefined}
                                      inputMode="numeric"
                                      pattern="[0-9]*"
                                      type="tel"
                                      maxLength={1}
                                      className="h-10 w-10 text-center text-lg font-bold rounded-lg border-gray-300 shadow-sm focus-visible:ring-[var(--theme-accent)]"
                                    />
                                  ))}
                                </div>
                                <Button
                                  type="button"
                                  size="sm"
                                  className="h-9 w-full rounded-lg bg-gray-900 text-white hover:bg-gray-800 text-xs"
                                  onClick={() => void verifyEmailOtp()}
                                  disabled={otpVerifying || otpDigits.some((d) => !d)}
                                >
                                  {otpVerifying ? 'Проверка…' : 'Подтвердить код'}
                                </Button>
                              </div>
                            ) : null}

                            {otpError && typeof otpError === 'string' ? (
                              <p className="text-[10px] text-[var(--theme-accent-text)] text-center">{otpError}</p>
                            ) : null}
                          </>
                        )}
                      </div>
                    ) : null}
                  </>
                ) : null}

                {/* Agreement UI temporarily disabled (keep announcement step). */}

                {/* Draft urgency countdown — hidden on mobile for compact checkout */}
                <div className="hidden sm:block">
                  <DraftCountdown text={configSteps.step4.draftTimerText} />
                </div>

                {/* Primary CTA after contact inputs */}
                <Button
                  type="submit"
                  className="h-12 w-full rounded-xl bg-[var(--theme-button)] text-sm font-bold text-white shadow-lg shadow-[var(--theme-accent-soft)] hover:bg-[var(--theme-button)] active:scale-95 transition-all"
                  disabled={
                    loading ||
                    (!manualConfirmationEnabled && emailEnabled && emailOtpEnabled && !emailVerified)
                  }
                >
                  {loading ? 'Обработка…' : (manualConfirmationEnabled ? configSteps.step4.manualCheckoutButtonText : `${configSteps.step4.checkoutButtonText}${funnelPriceVisibility.checkoutButton ? ` — ${fmtCurrency(discountedPaymentAmount)}` : ''}`)}
                </Button>

                {/* Trust badges */}
                <div className="flex justify-center gap-4 text-[10px] text-gray-400">
                  {configSteps.step4.securityBadges.map((badge, i) => (
                    <div key={i} className="flex items-center gap-1">
                      {i === 0 ? <ShieldCheck className="h-3 w-3" /> : <Clock className="h-3 w-3" />}
                      {badge.replace('{delivery}', deliveryEta.short)}
                    </div>
                  ))}
                </div>
                {error && (
                  <p className="text-center text-xs text-[var(--theme-accent-text)] font-medium">{error}</p>
                )}
              </div>

              {/* 3. Order Summary — always visible, no accordion */}
              <div className="border border-gray-200 rounded-xl overflow-hidden bg-white shadow-sm">
                <div className="flex items-center justify-between p-3 bg-gray-50 border-b border-gray-100">
                  <div className="flex items-center gap-2">
                    {configSteps.step4.orderSummaryEmoji && <span className="text-base">{configSteps.step4.orderSummaryEmoji}</span>}
                    <span className="text-xs font-bold text-gray-700">{configSteps.step4.orderSummaryLabel}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() => setStep(1)}
                    className="text-[10px] font-semibold text-[var(--theme-accent-text)] hover:underline flex items-center gap-0.5"
                  >
                    Изменить <ChevronRight className="h-3 w-3" />
                  </button>
                </div>
                <div className="p-4 text-xs space-y-2">
                  {funnelPriceVisibility.orderSummary && (
                    <>
                      <div className="flex justify-between items-center">
                        <span className="text-gray-500">Персональный трек <span className="text-gray-400">({relationship})</span></span>
                        {originalAmount > paymentAmount && !appliedDiscount && (
                          <span className="text-[10px] font-bold bg-[var(--theme-accent)] text-white px-2 py-0.5 rounded-full">СКИДКА {Math.round((1 - paymentAmount / originalAmount) * 100)}%</span>
                        )}
                      </div>
                      <div className="flex justify-between items-center pb-2 border-b border-gray-100">
                        <span className="text-gray-500">Итого</span>
                        <span className="font-bold text-[var(--theme-accent-text)] flex items-center gap-2">
                          {(originalAmount > effectivePaymentAmount || appliedDiscount) && (
                            <span className="text-gray-400 line-through font-normal">{fmtCurrency(appliedDiscount ? effectivePaymentAmount : originalAmount)}</span>
                          )}
                          {fmtCurrency(appliedDiscount ? discountedPaymentAmount : effectivePaymentAmount)}
                        </span>
                      </div>
                    </>
                  )}
                  <div className="flex justify-between">
                    <span className="text-gray-500">Для</span>
                    <span className="font-bold text-gray-900">{recipientName}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-500">Стиль и голос</span>
                    <span className="font-bold text-gray-900">
                      {(configSteps.step2.vibeChips.find((c) => c.id === genre)?.label ?? genre)}
                      {' ('}
                      {(() => {
                        const vs = form.getValues('musicPreferences.voiceStyle')
                        return configSteps.step2.voiceOptions.find((v) => v.value === vs)?.label ?? vs
                      })()}
                      {')'}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-500">Язык</span>
                    <span className="font-bold text-gray-900">
                      {configSteps.step2.languageOptions.find((o) => o.value === (language || 'Russian'))?.label
                        ?? language
                        ?? 'Русский'}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-500">Срок</span>
                    <span className="font-bold text-green-600 flex items-center gap-1"><Zap className="h-3 w-3" /> {deliveryEta.short}</span>
                  </div>
                  {funnelPriceVisibility.orderSummary && (
                    <>
                      <Separator className="my-2" />
                      <div className="space-y-2">
                        {/* Discount code section — collapsible */}
                        <div className="py-2">
                          {!appliedDiscount && (
                            <div className="rounded-lg border border-gray-200 overflow-hidden">
                              <button
                                type="button"
                                onClick={() => setDiscountExpanded(!discountExpanded)}
                                className="w-full flex items-center justify-between px-3 py-2.5 bg-gray-50 hover:bg-gray-100 transition-colors"
                              >
                                <span className="text-xs font-semibold text-gray-600 flex items-center gap-2">
                                  <Tag className="h-3.5 w-3.5" /> Есть промокод?
                                </span>
                                <ChevronDown className={`h-4 w-4 text-gray-400 transition-transform duration-200 ${discountExpanded ? 'rotate-180' : ''}`} />
                              </button>
                              {discountExpanded && (
                                <div className="space-y-2 p-3 bg-gray-50 border-t border-gray-200">
                                  <div className="flex gap-2">
                                    <Input
                                      value={discountCodeInput}
                                      onChange={e => { setDiscountCodeInput(e.target.value.toUpperCase()); setDiscountError(null) }}
                                      placeholder="Промокод"
                                      className="h-10 text-sm flex-1 uppercase font-semibold"
                                    />
                                    <Button
                                      type="button"
                                      variant="outline"
                                      className="h-10 px-3"
                                      title="Вставить из буфера"
                                      onClick={async () => {
                                        try {
                                          const text = await navigator.clipboard.readText()
                                          setDiscountCodeInput(text.toUpperCase().trim())
                                          setDiscountError(null)
                                        } catch {
                                          setDiscountError('Не удалось прочитать буфер обмена')
                                        }
                                      }}
                                    >
                                      <Clipboard className="h-4 w-4" />
                                    </Button>
                                    <Button
                                      type="button"
                                      className="h-10 text-sm px-4 bg-[var(--theme-accent)] text-white hover:bg-[var(--theme-accent)] font-semibold"
                                      onClick={() => void validateDiscountCode()}
                                      disabled={discountValidating || !discountCodeInput.trim()}
                                    >
                                      {discountValidating ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Применить'}
                                    </Button>
                                  </div>
                                  {discountError && <p className="text-xs text-red-600 text-center font-medium">{discountError}</p>}
                                </div>
                              )}
                            </div>
                          )}
                          {appliedDiscount && (
                            <div className="flex items-center justify-between bg-gradient-to-r from-green-50 to-green-100 border border-green-300 rounded-lg px-4 py-2.5 shadow-sm">
                              <div className="flex items-center gap-2">
                                <Tag className="h-4 w-4 text-green-600" />
                                <span className="text-sm font-bold text-green-800">{appliedDiscount.code}</span>
                                <span className="text-xs font-bold bg-green-600 text-white px-2 py-1 rounded-full">СКИДКА {appliedDiscount.type === 'percentage' ? `${appliedDiscount.percentageValue}%` : fmtCurrency(appliedDiscount.amount ?? 0)}</span>
                              </div>
                              <button
                                type="button"
                                onClick={() => { setAppliedDiscount(null); setDiscountCodeInput(''); setDiscountError(null) }}
                                className="text-xs text-red-600 hover:text-red-700 font-semibold hover:underline"
                              >
                                Убрать
                              </button>
                            </div>
                          )}
                        </div>
                      </div>
                    </>
                  )}
                </div>
              </div>

              {/* 5. Mini sample songs playlist */}
              {checkoutExtraData.playlist.length > 0 && (
                <div className="space-y-2">
                  <div className="text-sm font-bold text-gray-800 text-center flex items-center justify-center gap-1.5"><Music className="h-4 w-4" /> Примеры готовых треков</div>
                  <audio
                    ref={miniPlayerRef}
                    onEnded={() => setPlayingTrackIdx(null)}
                    onError={() => setPlayingTrackIdx(null)}
                    onTimeUpdate={(e) => {
                      const a = e.currentTarget
                      if (a.duration > 0) setMiniProgress((a.currentTime / a.duration) * 100)
                    }}
                    className="hidden"
                  />
                  <div className="space-y-2">
                    {checkoutExtraData.playlist.slice(0, 3).map((track, i) => {
                      const isPlaying = playingTrackIdx === i
                      const audioSrc = track.audioUrl ? resolveAsset(track.audioUrl) : ''
                      return (
                        <div
                          key={i}
                          role="button"
                          onClick={() => {
                            const audio = miniPlayerRef.current
                            if (!audio || !audioSrc) return
                            if (isPlaying) {
                              audio.pause()
                              setPlayingTrackIdx(null)
                            } else {
                              audio.src = audioSrc
                              audio.play().catch(() => setPlayingTrackIdx(null))
                              setMiniProgress(0)
                              setPlayingTrackIdx(i)
                            }
                          }}
                          className={`w-full flex items-center gap-3 bg-white rounded-xl border border-gray-100 p-3 shadow-sm hover:bg-gray-50 transition-colors text-left ${audioSrc ? 'cursor-pointer' : 'opacity-40'}`}
                        >
                          <div className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-full ${isPlaying ? 'bg-[var(--theme-accent)] text-white' : 'bg-[var(--theme-accent-soft)] text-[var(--theme-accent-text)]'} transition-colors`}>
                            {isPlaying ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5 ml-0.5" />}
                          </div>
                          <div className="flex-1 min-w-0">
                            <div className="text-xs font-bold text-gray-900 truncate">{track.title}</div>
                            {track.subtitle && <div className="text-[10px] text-gray-400 truncate">{track.subtitle}</div>}
                            {isPlaying && (
                              <div className="mt-1.5 flex" onClick={(e) => e.stopPropagation()}>
                                <SeekBar
                                  progressPct={miniProgress}
                                  trackColor="rgba(0,0,0,.08)"
                                  fillColor="var(--theme-accent)"
                                  onSeek={(pct) => {
                                    const a = miniPlayerRef.current
                                    if (!a || !(a.duration > 0)) return
                                    a.currentTime = pct * a.duration
                                    setMiniProgress(pct * 100)
                                  }}
                                />
                              </div>
                            )}
                          </div>
                          <Music className="h-3.5 w-3.5 text-gray-300 shrink-0" />
                        </div>
                      )
                    })}
                  </div>
                </div>
              )}

              {/* 6. Guarantee box */}
              <div className="rounded-xl border border-green-200 bg-green-50 p-4 text-center space-y-2">
                <div className="inline-flex items-center gap-1 text-[10px] font-bold text-green-700 bg-green-100 px-2 py-0.5 rounded-full">
                  <BadgeCheck className="h-3 w-3" /> {checkoutExtraData.guarantee.badge}
                </div>
                <div className="text-sm font-bold text-green-900">{checkoutExtraData.guarantee.headline}</div>
                <p className="text-xs text-green-800 leading-relaxed" dangerouslySetInnerHTML={{ __html: checkoutExtraData.guarantee.description }} />
              </div>

              {/* 7. What You'll Get checklist */}
              <div className="space-y-3">
                <p className="text-center text-xs text-gray-400">{configSteps.step4.checkoutSubtext.replace('{recipient}', currentRecipient)}</p>

                <div className="rounded-xl bg-gray-50 p-3 border border-gray-100 space-y-2">
                  <div className="text-[10px] font-bold text-gray-500 uppercase tracking-wider">{configSteps.step4.nextStepsTitle}</div>
                  <div className="space-y-1.5 text-xs text-gray-700">
                    {(manualConfirmationEnabled ? configSteps.step4.manualNextSteps : configSteps.step4.nextSteps).map((s, i) => (
                      <div key={i} className="flex gap-2 items-start">
                        <div className="flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-[var(--theme-accent-soft)] text-[9px] font-bold text-[var(--theme-accent-text)] mt-0.5">
                          <Check className="h-2.5 w-2.5" />
                        </div>
                        <span className="leading-tight" dangerouslySetInnerHTML={{ __html: s.text.replace('{delivery}', `<span class="font-bold">${deliveryEta.sentenceLower}</span>`) }} />
                      </div>
                    ))}
                  </div>
                </div>
              </div>

              {/* 8. FAQ accordion */}
              {checkoutExtraData.faqItems.length > 0 && (
                <div className="space-y-2">
                  <div className="text-[10px] font-bold text-gray-500 uppercase tracking-wider">Вопросы</div>
                  <div className="space-y-1.5">
                    {checkoutExtraData.faqItems.map((item, i) => (
                      <div key={i} className="border border-gray-100 rounded-xl overflow-hidden bg-white">
                        <button
                          type="button"
                          onClick={() => setFaqOpenIndex(faqOpenIndex === i ? null : i)}
                          className="w-full flex items-center justify-between p-3 text-left hover:bg-gray-50 transition-colors"
                        >
                          <span className="text-xs font-semibold text-gray-800 pr-2">{item.q}</span>
                          <ChevronDown className={`h-3.5 w-3.5 text-gray-400 shrink-0 transition-transform ${faqOpenIndex === i ? 'rotate-180' : ''}`} />
                        </button>
                        {faqOpenIndex === i && (
                          <div className="px-3 pb-3 text-xs text-gray-600 leading-relaxed animate-in fade-in slide-in-from-top-1 duration-200">
                            {item.a}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* 9. Back button */}
              <div className="text-center">
                <button
                  type="button"
                  onClick={handleBack}
                  className="text-xs text-gray-400 hover:text-gray-600 transition-colors inline-flex items-center gap-1"
                >
                  <ChevronLeft className="h-3 w-3" /> Назад
                </button>
              </div>

            </div>
          )}

          {/* BOTTOM NAV / CTA — hidden on checkout step and package step (inline CTAs used instead) */}
          <div className={`fixed inset-x-0 bottom-0 z-50 bg-white border-t border-gray-100 px-4 pt-4 pb-[calc(1rem+env(safe-area-inset-bottom))] shadow-[0_-4px_6px_-1px_rgba(0,0,0,0.05)]${step === CHECKOUT_STEP || (step === PACKAGE_STEP && packageStepEnabled) ? ' hidden' : ''}`}>
             <div className="mx-auto max-w-4xl flex flex-col gap-2">
               {/* Intentionally no global error banner here (avoid duplicate warnings). */}

               <div className="flex gap-3">
                 {step > 0 && (
                   <Button
                     type="button"
                     variant="outline"
                     size="lg"
                     className="shrink-0 h-12 sm:h-14 px-4 sm:px-8 rounded-xl border-gray-200 hover:bg-gray-50 active:scale-95 transition-all"
                     onClick={step === minStep ? () => navigate(themeSlug ? `/${themeSlug}` : '/') : handleBack}
                     disabled={loading}
                   >
                     <ChevronLeft className="h-5 w-5" />
                     <span className="hidden sm:inline ml-2 font-bold">Назад</span>
                   </Button>
                 )}

                 <Button
                   type="submit"
                   className="h-12 sm:h-14 w-full rounded-xl bg-[var(--theme-button)] text-base sm:text-lg font-bold text-white shadow-lg shadow-[var(--theme-accent-soft)] hover:bg-[var(--theme-button)] active:scale-95 transition-all"
                   onClick={step < CHECKOUT_STEP ? (e) => { e.preventDefault(); handleNext(); } : undefined}
                  disabled={
                    loading ||
                    (step === CHECKOUT_STEP && !manualConfirmationEnabled && emailEnabled && emailOtpEnabled && !emailVerified) ||
                    false
                  }
                 >
                   {step === 0 && configSteps.step0.enabled ? 'Начать ->' :
                    step === 1 ? 'Стиль и звук ->' :
                    step === 2 ? 'Добавить историю ->' :
                    step === 3 ? 'Почти готово! ->' :
                    loading ? 'Обработка…' : (manualConfirmationEnabled ? configSteps.step4.manualCheckoutButtonText : (emailEnabled && emailOtpEnabled) ? (emailVerified ? `${configSteps.step4.checkoutButtonText}${funnelPriceVisibility.checkoutButton ? ` — ${fmtCurrency(discountedPaymentAmount)}` : ''}` : 'Подтвердите email') : `${configSteps.step4.checkoutButtonText}${funnelPriceVisibility.checkoutButton ? ` — ${fmtCurrency(discountedPaymentAmount)}` : ''}`)}
                 </Button>
               </div>
             </div>
          </div>
          {/* NOTE: Error banner is shown inside the fixed CTA area above. */}

        </form>
      </main>
    </div>
  )
}
