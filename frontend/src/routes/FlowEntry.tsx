import { lazy, Suspense, useEffect } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Loader2 } from 'lucide-react'

import { useFlowConfig } from '@/flow/useFlowConfig'
import { FlowRoute } from '@/flow/FlowRoute'

// Легаси-форма грузится лениво: в плечах варианта 1 она не нужна вовсе, и её
// бандл не должен попадать в путь покупателя.
const ConfigRoute = lazy(() => import('@/routes/ConfigRoute').then((m) => ({ default: m.ConfigRoute })))

/**
 * Вход в /config: сервер решает, какое плечо A/B-теста показать этой сессии.
 *
 * Если /api/public/flow недоступен (или отдал пустой список шагов) — работает
 * прежняя форма. Новый флоу не должен уметь обрушить продажи целиком.
 */
export function FlowEntry() {
  const state = useFlowConfig()
  const { pathname, search } = useLocation()
  const navigate = useNavigate()

  // Назначенное плечо показывается в адресе (/config?arm=variant1-990): команда
  // видит, куда попала, и ссылку можно скопировать. Сервер это же плечо
  // сессии заново не закрепляет, ротация не ломается.
  //
  // В адрес пишется ИМЯ плеча (armName), а не технический id: имя стоит в
  // рекламных ссылках, и раньше клик по кнопке лендинга подменял `?arm=` на id
  // — в GA и Метрике всплывали адреса вида ?arm=v1_price, которых нет ни в
  // одной кампании.
  const armName =
    state.status === 'ready' && !state.config.route
      ? state.config.armName || state.config.flowVariant
      : null
  useEffect(() => {
    if (!armName) return
    const params = new URLSearchParams(search)
    if (params.get('arm') === armName) return
    params.set('arm', armName)
    navigate({ pathname, search: `?${params.toString()}` }, { replace: true })
  }, [armName, pathname, search, navigate])

  if (state.status === 'loading') {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-gray-400" />
      </div>
    )
  }

  if (state.status === 'unavailable') {
    return (
      <Suspense fallback={<div className="flex min-h-screen items-center justify-center"><Loader2 className="h-8 w-8 animate-spin text-gray-400" /></div>}>
        <ConfigRoute />
      </Suspense>
    )
  }

  // Плечи варианта 2 живут на движке /create — сервер присылает их путь.
  // ?v закрепится там через variant.ts, так что человек останется в своём
  // варианте и при прямом возврате на /create.
  if (state.config.route) {
    return <Navigate to={state.config.route} replace />
  }

  return <FlowRoute config={state.config} sessionId={state.sessionId} />
}
