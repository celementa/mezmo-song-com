import { lazy, Suspense } from 'react'
import {
  createBrowserRouter,
  Navigate,
  useLocation,
  useRouteError,
  isRouteErrorResponse,
  type RouteObject,
} from 'react-router-dom'

import { LandingRoute } from '@/routes/LandingRoute'
import { PublicRoot } from '@/features/analytics/PublicRoot'
import { ThemeProvider } from '@/features/theme/ThemeContext'
import { localePath, preferredLang, tr } from '@/lib/lang'

// mezmo-song.com: лендинг, две воронки заказа (/config — анкета, потом оплата;
// /create — оплата в начале), страница заказа, юридические страницы и админка.
// Канонические адреса — с языком: /ru/… и /en/…. Корень редиректит по куке или
// Accept-Language; ссылка с языком в пути не перебивается.
const FlowEntry = lazy(() => import('@/routes/FlowEntry').then((m) => ({ default: m.FlowEntry })))
const CreateRoute = lazy(() => import('@/routes/create/CreateRoute').then((m) => ({ default: m.CreateRoute })))
const OrderDeliveryRoute = lazy(() => import('@/routes/OrderDeliveryRoute').then((m) => ({ default: m.OrderDeliveryRoute })))
const ThanksRoute = lazy(() => import('@/routes/ThanksRoute').then((m) => ({ default: m.ThanksRoute })))
const AdminOrdersRoute = lazy(() => import('@/routes/AdminOrdersRoute').then((m) => ({ default: m.AdminOrdersRoute })))
const AdminAnalyticsRoute = lazy(() => import('@/routes/AdminAnalyticsRoute').then((m) => ({ default: m.AdminAnalyticsRoute })))
const AdminSettingsRoute = lazy(() => import('@/routes/AdminSettingsRoute').then((m) => ({ default: m.AdminSettingsRoute })))
const AdminEmailsRoute = lazy(() => import('@/routes/AdminEmailsRoute').then((m) => ({ default: m.AdminEmailsRoute })))
const LegalPage = lazy(() => import('@/routes/LegalPage').then((m) => ({ default: m.LegalPage })))
const ContactPage = lazy(() => import('@/routes/ContactPage').then((m) => ({ default: m.ContactPage })))
const PayStripeRoute = lazy(() => import('@/routes/PayStripeRoute').then((m) => ({ default: m.PayStripeRoute })))
const PayYookassaRoute = lazy(() => import('@/routes/PayYookassaRoute').then((m) => ({ default: m.PayYookassaRoute })))
const PayUnitpayRoute = lazy(() => import('@/routes/PayUnitpayRoute').then((m) => ({ default: m.PayUnitpayRoute })))
// Подписка: страницы только русские (решение от 2026-11-13), подписка в общий
// двуязычный набор contentPages не входит — см. subscriptionPages ниже.
const SubscribeRoute = lazy(() => import('@/routes/SubscribeRoute').then((m) => ({ default: m.SubscribeRoute })))
const SubscribeReturnRoute = lazy(() => import('@/routes/SubscribeReturnRoute').then((m) => ({ default: m.SubscribeReturnRoute })))
const AccountRoute = lazy(() => import('@/routes/AccountRoute').then((m) => ({ default: m.AccountRoute })))
const AccountNewRoute = lazy(() => import('@/routes/AccountNewRoute').then((m) => ({ default: m.AccountNewRoute })))
const SubscriptionCancelRoute = lazy(() => import('@/routes/SubscriptionCancelRoute').then((m) => ({ default: m.SubscriptionCancelRoute })))
const SubscriptionOfferRoute = lazy(() => import('@/routes/SubscriptionOfferRoute').then((m) => ({ default: m.SubscriptionOfferRoute })))

function LazyFallback() {
  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="h-8 w-8 animate-spin rounded-full border-4 border-gray-200 border-t-[var(--theme-accent,#D4AF37)]" />
    </div>
  )
}

function RootRedirect() {
  const { search, hash } = useLocation()
  return <Navigate to={{ pathname: `/${preferredLang()}`, search, hash }} replace />
}

function RouteErrorFallback() {
  const error = useRouteError()
  const is404 = isRouteErrorResponse(error) && error.status === 404

  return (
    <div className="flex min-h-screen flex-col items-center justify-center px-4 text-center">
      <h1 className="text-4xl font-bold text-gray-800">{is404 ? '404' : tr('Ошибка', 'Error')}</h1>
      <p className="mt-2 text-gray-600">
        {is404 ? tr('Страница не найдена.', 'Page not found.') : tr('Что-то пошло не так. Попробуйте ещё раз.', 'Something went wrong. Please try again.')}
      </p>
      <button
        type="button"
        onClick={() => window.location.assign(localePath('/'))}
        className="mt-6 rounded-lg bg-[var(--theme-accent,#D4AF37)] px-6 py-2.5 text-sm font-semibold text-white hover:opacity-90"
      >
        {tr('На главную', 'Back to home')}
      </button>
    </div>
  )
}

// Главная и /start — один лендинг в двух режимах (с ценами и без). Какой режим
// у главной, решает настройка «Цены на главной» в админке, поэтому маршрут один
// на оба адреса, а режим выбирает LandingRoute по текущему пути.
function landing() {
  return (
    <ThemeProvider themeSlug={null}>
      <LandingRoute />
    </ThemeProvider>
  )
}

// Страницы без корня и без языкового префикса. Корень и /ru, /en добавляются
// отдельно: у корня редирект, у языковых версий — тот же набор страниц.
const contentPages: RouteObject[] = [
  {
    // Копия главной: цены показывает либо главная, либо этот адрес — зависит от
    // настройки «Цены на главной» в админке.
    path: '/start',
    element: landing(),
  },
  {
    path: '/config',
    element: (
      <Suspense fallback={<LazyFallback />}>
        <ThemeProvider themeSlug={null}>
          <FlowEntry />
        </ThemeProvider>
      </Suspense>
    ),
  },
  {
    // Воронка «оплата в начале» (плечи variant4): FlowEntry уводит сюда по
    // route плеча, ?v=2 закрепляется в localStorage.
    path: '/create',
    element: (
      <Suspense fallback={<LazyFallback />}>
        <CreateRoute />
      </Suspense>
    ),
  },
  { path: '/thanks', element: <Suspense fallback={<LazyFallback />}><ThanksRoute /></Suspense> },
  {
    // Форма Stripe на отдельном экране: на странице только виджет провайдера.
    path: '/pay/stripe',
    element: (
      <Suspense fallback={<LazyFallback />}>
        <ThemeProvider themeSlug={null}>
          <PayStripeRoute />
        </ThemeProvider>
      </Suspense>
    ),
  },
  {
    // Форма ЮKassa на отдельном экране: без автоскролла и сводки заказа.
    path: '/pay/yookassa',
    element: (
      <Suspense fallback={<LazyFallback />}>
        <ThemeProvider themeSlug={null}>
          <PayYookassaRoute />
        </ThemeProvider>
      </Suspense>
    ),
  },
  {
    // Форма UnitPay на отдельном экране: iframe провайдера на весь экран.
    path: '/pay/unitpay',
    element: (
      <Suspense fallback={<LazyFallback />}>
        <ThemeProvider themeSlug={null}>
          <PayUnitpayRoute />
        </ThemeProvider>
      </Suspense>
    ),
  },
  { path: '/order/:orderId', element: <Suspense fallback={<LazyFallback />}><OrderDeliveryRoute /></Suspense> },
  { path: '/terms', element: <Suspense fallback={<LazyFallback />}><LegalPage doc="terms" /></Suspense> },
  { path: '/privacy', element: <Suspense fallback={<LazyFallback />}><LegalPage doc="privacy" /></Suspense> },
  { path: '/security', element: <Suspense fallback={<LazyFallback />}><LegalPage doc="security" /></Suspense> },
  { path: '/consent', element: <Suspense fallback={<LazyFallback />}><LegalPage doc="consent" /></Suspense> },
  { path: '/contacts', element: <Suspense fallback={<LazyFallback />}><ContactPage /></Suspense> },
]

const localizedPages: RouteObject[] = (['ru', 'en'] as const).flatMap((lang) => [
  // Главная: режим цен выбирает настройка «Цены на главной» в админке, а
  // вторая версия лендинга живёт на /:lang/start.
  { path: `/${lang}`, element: landing() },
  ...contentPages.map((page) => ({ ...page, path: `/${lang}${page.path}` })),
])

// RU-страницы подписки. В contentPages их не кладём: localizedPages размножил бы
// их на /en, а подписка — только русская. Адреса работают и без префикса, и под
// /ru; /en/subscribe остаётся 404 (маршрута нет).
const subscriptionPages: RouteObject[] = [
  { path: '/subscribe', element: <Suspense fallback={<LazyFallback />}><SubscribeRoute /></Suspense> },
  { path: '/subscribe/return', element: <Suspense fallback={<LazyFallback />}><SubscribeReturnRoute /></Suspense> },
  { path: '/account', element: <Suspense fallback={<LazyFallback />}><AccountRoute /></Suspense> },
  { path: '/account/new', element: <Suspense fallback={<LazyFallback />}><AccountNewRoute /></Suspense> },
  { path: '/subscription/cancel', element: <Suspense fallback={<LazyFallback />}><SubscriptionCancelRoute /></Suspense> },
  { path: '/subscription-offer', element: <Suspense fallback={<LazyFallback />}><SubscriptionOfferRoute /></Suspense> },
]

export const router = createBrowserRouter([
  {
    element: <PublicRoot />,
    errorElement: <RouteErrorFallback />,
    children: [
      { path: '/', element: <RootRedirect /> },
      ...localizedPages,
      ...subscriptionPages,
      ...subscriptionPages.map((p) => ({ ...p, path: `/ru${p.path}` })),
      // Старые ссылки без языка продолжают открываться: их уже разослали в
      // письмах и кампаниях. Новые ссылки всегда строятся с /ru или /en.
      ...contentPages,
    ],
  },
  // Страницы админки Go отдаёт только после входа (cmd/server/adminauth.go).
  { path: '/admin/orders', element: <Suspense fallback={<LazyFallback />}><AdminOrdersRoute /></Suspense>, errorElement: <RouteErrorFallback /> },
  { path: '/admin/analytics', element: <Suspense fallback={<LazyFallback />}><AdminAnalyticsRoute /></Suspense>, errorElement: <RouteErrorFallback /> },
  { path: '/admin/settings', element: <Suspense fallback={<LazyFallback />}><AdminSettingsRoute /></Suspense>, errorElement: <RouteErrorFallback /> },
  { path: '/admin/emails', element: <Suspense fallback={<LazyFallback />}><AdminEmailsRoute /></Suspense>, errorElement: <RouteErrorFallback /> },
])
