import { lazy, Suspense } from 'react'
import { createBrowserRouter, useRouteError, isRouteErrorResponse } from 'react-router-dom'

import { LandingRoute } from '@/routes/LandingRoute'
import { PublicRoot } from '@/features/analytics/PublicRoot'
import { ThemeProvider } from '@/features/theme/ThemeContext'

// /config ведёт в FlowEntry: он выбирает плечо A/B-теста и при недоступности
// /api/public/flow откатывается на прежнюю форму. Прямые ссылки на ConfigRoute
// остались у режимов подписчика и админки — там плечо не при чём.
const FlowEntry = lazy(() => import('@/routes/FlowEntry').then((m) => ({ default: m.FlowEntry })))
const ConfigRoute = lazy(() => import('@/routes/ConfigRoute').then((m) => ({ default: m.ConfigRoute })))
const CheckoutRoute = lazy(() => import('@/routes/CheckoutRoute').then((m) => ({ default: m.CheckoutRoute })))
const PrivacyPage = lazy(() => import('@/routes/PrivacyPage').then((m) => ({ default: m.PrivacyPage })))
const TermsPage = lazy(() => import('@/routes/TermsPage').then((m) => ({ default: m.TermsPage })))
const ContactPage = lazy(() => import('@/routes/ContactPage').then((m) => ({ default: m.ContactPage })))
const OrderDeliveryRoute = lazy(() => import('@/routes/OrderDeliveryRoute').then((m) => ({ default: m.OrderDeliveryRoute })))
const ThanksRoute = lazy(() => import('@/routes/ThanksRoute').then((m) => ({ default: m.ThanksRoute })))
const AdminOrdersRoute = lazy(() => import('@/routes/AdminOrdersRoute').then((m) => ({ default: m.AdminOrdersRoute })))
const AdminAnalyticsRoute = lazy(() => import('@/routes/AdminAnalyticsRoute').then((m) => ({ default: m.AdminAnalyticsRoute })))
const AdminSettingsRoute = lazy(() => import('@/routes/AdminSettingsRoute').then((m) => ({ default: m.AdminSettingsRoute })))
const AdminEmailsRoute = lazy(() => import('@/routes/AdminEmailsRoute').then((m) => ({ default: m.AdminEmailsRoute })))
const AdminLesrosesRoute = lazy(() => import('@/routes/AdminLesrosesRoute').then((m) => ({ default: m.AdminLesrosesRoute })))
const OldDesignRoute = lazy(() => import('@/routes/OldDesignRoute').then((m) => ({ default: m.OldDesignRoute })))
const CreateRoute = lazy(() => import('@/routes/create/CreateRoute').then((m) => ({ default: m.CreateRoute })))
const SubscribeRoute = lazy(() => import('@/routes/SubscribeRoute').then((m) => ({ default: m.SubscribeRoute })))
const SubscribeReturnRoute = lazy(() => import('@/routes/SubscribeReturnRoute').then((m) => ({ default: m.SubscribeReturnRoute })))
const AccountRoute = lazy(() => import('@/routes/AccountRoute').then((m) => ({ default: m.AccountRoute })))
const SubscriptionCancelRoute = lazy(() => import('@/routes/SubscriptionCancelRoute').then((m) => ({ default: m.SubscriptionCancelRoute })))
const SubscriptionOfferRoute = lazy(() => import('@/routes/SubscriptionOfferRoute').then((m) => ({ default: m.SubscriptionOfferRoute })))

function LazyFallback() {
  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="h-8 w-8 animate-spin rounded-full border-4 border-gray-200 border-t-[var(--theme-accent,#D4AF37)]" />
    </div>
  )
}

function RouteErrorFallback() {
  const error = useRouteError()
  const is404 = isRouteErrorResponse(error) && error.status === 404

  return (
    <div className="flex min-h-screen flex-col items-center justify-center px-4 text-center">
      <h1 className="text-4xl font-bold text-gray-800">{is404 ? '404' : 'Ошибка'}</h1>
      <p className="mt-2 text-gray-600">
        {is404 ? 'Страница не найдена.' : 'Что-то пошло не так. Попробуйте ещё раз.'}
      </p>
      <button
        type="button"
        onClick={() => window.location.assign('/')}
        className="mt-6 rounded-lg bg-[var(--theme-accent,#D4AF37)] px-6 py-2.5 text-sm font-semibold text-white hover:opacity-90"
      >
        На главную
      </button>
    </div>
  )
}

export const router = createBrowserRouter([
  {
    element: <PublicRoot />,
    errorElement: <RouteErrorFallback />,
    children: [
      {
        path: '/',
        element: (
          <ThemeProvider themeSlug={null}>
            <LandingRoute />
          </ThemeProvider>
        ),
      },
      {
        path: '/old-design',
        element: (
          <Suspense fallback={<LazyFallback />}>
            <ThemeProvider themeSlug={null}>
              <OldDesignRoute />
            </ThemeProvider>
          </Suspense>
        ),
      },
      {
        // Новая воронка: вариант (v1/v2) назначает серверная ротация плеч
        // (та же, что на /config) и закрепляется
        // за посетителем. Принудительно — /create?v=1 и /create?v=2.
        path: '/create',
        element: (
          <Suspense fallback={<LazyFallback />}>
            <CreateRoute />
          </Suspense>
        ),
      },
      {
        // Прежняя воронка целиком, как была. Сюда же ведут внутренние режимы,
        // которым нужен старый визард (см. /admin/ads, /account/new).
        path: '/old-flow',
        element: (
          <Suspense fallback={<LazyFallback />}>
            <ThemeProvider themeSlug={null}>
              <ConfigRoute />
            </ThemeProvider>
          </Suspense>
        ),
      },
      {
        // Старый адрес воронки (уехавшие рекламные ссылки и закладки): FlowEntry
        // спрашивает /api/public/flow и рисует плечо A/B-теста, а при
        // недоступности ручки — прежнюю форму.
        path: '/config',
        element: (
          <Suspense fallback={<LazyFallback />}>
            <ThemeProvider themeSlug={null}>
              <FlowEntry />
            </ThemeProvider>
          </Suspense>
        ),
      },
      { path: '/checkout', element: <Suspense fallback={<LazyFallback />}><CheckoutRoute /></Suspense> },
      { path: '/thanks', element: <Suspense fallback={<LazyFallback />}><ThanksRoute /></Suspense> },
      {
        path: '/order/:orderId',
        element: (
          <Suspense fallback={<LazyFallback />}>
            <OrderDeliveryRoute />
          </Suspense>
        ),
      },
      { path: '/subscribe', element: <Suspense fallback={<LazyFallback />}><SubscribeRoute /></Suspense> },
      { path: '/subscribe/return', element: <Suspense fallback={<LazyFallback />}><SubscribeReturnRoute /></Suspense> },
      { path: '/account', element: <Suspense fallback={<LazyFallback />}><AccountRoute /></Suspense> },
      {
        path: '/account/new',
        element: (
          <Suspense fallback={<LazyFallback />}>
            <ThemeProvider themeSlug={null}>
              <ConfigRoute subscriberMode />
            </ThemeProvider>
          </Suspense>
        ),
      },
      { path: '/subscription/cancel', element: <Suspense fallback={<LazyFallback />}><SubscriptionCancelRoute /></Suspense> },
      { path: '/subscription-offer', element: <Suspense fallback={<LazyFallback />}><SubscriptionOfferRoute /></Suspense> },
      { path: '/privacy', element: <Suspense fallback={<LazyFallback />}><PrivacyPage /></Suspense> },
      { path: '/terms', element: <Suspense fallback={<LazyFallback />}><TermsPage /></Suspense> },
      { path: '/contacts', element: <Suspense fallback={<LazyFallback />}><ContactPage /></Suspense> },
    ],
  },
  {
    path: '/admin/orders',
    element: <Suspense fallback={<LazyFallback />}><AdminOrdersRoute /></Suspense>,
    errorElement: <RouteErrorFallback />,
  },
  {
    path: '/admin/analytics',
    element: <Suspense fallback={<LazyFallback />}><AdminAnalyticsRoute /></Suspense>,
    errorElement: <RouteErrorFallback />,
  },
  {
    path: '/admin/settings',
    element: <Suspense fallback={<LazyFallback />}><AdminSettingsRoute /></Suspense>,
    errorElement: <RouteErrorFallback />,
  },
  {
    path: '/admin/emails',
    element: <Suspense fallback={<LazyFallback />}><AdminEmailsRoute /></Suspense>,
    errorElement: <RouteErrorFallback />,
  },
  {
    path: '/admin/lesroses',
    element: <Suspense fallback={<LazyFallback />}><AdminLesrosesRoute /></Suspense>,
    errorElement: <RouteErrorFallback />,
  },
  {
    path: '/admin/ads',
    element: (
      <Suspense fallback={<LazyFallback />}>
        <ThemeProvider themeSlug={null}>
          <ConfigRoute adminMode />
        </ThemeProvider>
      </Suspense>
    ),
    errorElement: <RouteErrorFallback />,
  },
])
