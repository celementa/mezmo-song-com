import { useEffect } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Loader2 } from 'lucide-react'

import { useFlowConfig } from '@/flow/useFlowConfig'
import { FlowRoute } from '@/flow/FlowRoute'

/**
 * Вход в /config: сервер решает, какое плечо A/B-теста показать этой сессии.
 *
 * Легаси-формы, на которую mezmo.ru откатывается при недоступном
 * /api/public/flow, у .com нет: без сервера заказ не создать, поэтому человек
 * видит просьбу обновить страницу.
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
      <div className="flex min-h-screen flex-col items-center justify-center px-4 text-center">
        <p className="text-lg font-semibold text-gray-800">Не удалось загрузить оформление заказа</p>
        <p className="mt-2 text-sm text-gray-600">Проверьте соединение и обновите страницу.</p>
        <button
          type="button"
          onClick={() => window.location.reload()}
          className="mt-6 rounded-lg bg-[var(--theme-accent,#D4AF37)] px-6 py-2.5 text-sm font-semibold text-white hover:opacity-90"
        >
          Обновить
        </button>
      </div>
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
