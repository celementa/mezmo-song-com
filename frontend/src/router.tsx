import { lazy, Suspense } from 'react'
import { createBrowserRouter, useRouteError, isRouteErrorResponse } from 'react-router-dom'

import { LandingRoute } from '@/routes/LandingRoute'
import { PublicRoot } from '@/features/analytics/PublicRoot'
import { ThemeProvider } from '@/features/theme/ThemeContext'

// mezmo-song.com — статика без бэкенда: лендинг, анкета заказа и юридические
// страницы. Заказов, оплаты, кабинета и админки здесь нет — их маршруты из
// ai-songs не переносились.
const IntlFlowRoute = lazy(() => import('@/flow/IntlFlowRoute').then((m) => ({ default: m.IntlFlowRoute })))
const LegalPage = lazy(() => import('@/routes/LegalPage').then((m) => ({ default: m.LegalPage })))
const ContactPage = lazy(() => import('@/routes/ContactPage').then((m) => ({ default: m.ContactPage })))

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
        path: '/config',
        element: (
          <Suspense fallback={<LazyFallback />}>
            <ThemeProvider themeSlug={null}>
              <IntlFlowRoute />
            </ThemeProvider>
          </Suspense>
        ),
      },
      { path: '/terms', element: <Suspense fallback={<LazyFallback />}><LegalPage doc="terms" /></Suspense> },
      { path: '/privacy', element: <Suspense fallback={<LazyFallback />}><LegalPage doc="privacy" /></Suspense> },
      { path: '/security', element: <Suspense fallback={<LazyFallback />}><LegalPage doc="security" /></Suspense> },
      { path: '/consent', element: <Suspense fallback={<LazyFallback />}><LegalPage doc="consent" /></Suspense> },
      { path: '/contacts', element: <Suspense fallback={<LazyFallback />}><ContactPage /></Suspense> },
    ],
  },
])
