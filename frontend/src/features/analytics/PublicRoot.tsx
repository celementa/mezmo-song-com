import { useEffect, useState } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import { MetaPixel } from './MetaPixel'
import { TikTokPixel } from './TikTokPixel'
import { cachedSettingsGet } from '@/lib/settingsCache'
import { isLocalhost } from '@/lib/isLocalhost'
import { captureAttribution } from '@/lib/attribution'
import type { Loose } from '@/lib/loose'
import { MaintenancePage } from '@/routes/MaintenancePage'

type MaintenanceData = {
  enabled: boolean
  headline: string | null
  message: string | null
  email: string | null
  instagramUrl: string | null
  followText: string | null
}

function getInlinedMaintenance(): MaintenanceData | null {
  const inlined = (window as unknown as Loose).__SETTINGS__
  if (!inlined || typeof inlined !== 'object') return null
  return {
    enabled: Boolean(inlined.maintenanceEnabled),
    headline: inlined.maintenanceHeadline ?? null,
    message: inlined.maintenanceMessage ?? null,
    email: inlined.maintenanceEmail ?? null,
    instagramUrl: inlined.maintenanceInstagramUrl ?? null,
    followText: inlined.maintenanceFollowText ?? null,
  }
}

export function PublicRoot() {
  const location = useLocation()
  const isOrderRoute = location.pathname.startsWith('/order/')
  const [pixelId, setPixelId] = useState<string | null>(null)
  const [tiktokPixelId, setTiktokPixelId] = useState<string | null>(null)
  const [maintenance, setMaintenance] = useState<MaintenanceData>(() => {
    return (
      getInlinedMaintenance() ?? {
        enabled: false,
        headline: null,
        message: null,
        email: null,
        instagramUrl: null,
        followText: null,
      }
    )
  })

  // Снимаем рекламные метки с посадочного URL до любой навигации внутри SPA.
  useEffect(() => {
    captureAttribution()
  }, [])

  useEffect(() => {
    // На localhost не подключаем пиксели и head-скрипты — чтобы тесты не слали события.
    if (isLocalhost()) return
    cachedSettingsGet<{
      metaPixelId: string | null
      tiktokPixelId: string | null
      headScripts: string | null
      maintenanceEnabled?: boolean
      maintenanceHeadline?: string | null
      maintenanceMessage?: string | null
      maintenanceEmail?: string | null
      maintenanceInstagramUrl?: string | null
      maintenanceFollowText?: string | null
    }>('/api/public/settings')
      .then((data) => {
        setPixelId(data.metaPixelId ?? null)
        setTiktokPixelId(data.tiktokPixelId ?? null)
        setMaintenance({
          enabled: Boolean(data.maintenanceEnabled),
          headline: data.maintenanceHeadline ?? null,
          message: data.maintenanceMessage ?? null,
          email: data.maintenanceEmail ?? null,
          instagramUrl: data.maintenanceInstagramUrl ?? null,
          followText: data.maintenanceFollowText ?? null,
        })
        if (data.headScripts) {
          setTimeout(() => injectHeadScripts(data.headScripts!), 3500)
        }
      })
      .catch(() => {})
  }, [])

  if (!isOrderRoute && maintenance.enabled) {
    return (
      <MaintenancePage
        headline={maintenance.headline}
        message={maintenance.message}
        email={maintenance.email}
        instagramUrl={maintenance.instagramUrl}
        followText={maintenance.followText}
      />
    )
  }

  return (
    <>
      {pixelId && <MetaPixel pixelId={pixelId} />}
      {tiktokPixelId && <TikTokPixel pixelId={tiktokPixelId} />}
      <Outlet />
    </>
  )
}

function injectHeadScripts(html: string) {
  const container = document.createElement('div')
  container.innerHTML = html
  Array.from(container.childNodes).forEach((node) => {
    if (node.nodeName === 'SCRIPT') {
      const orig = node as HTMLScriptElement
      const script = document.createElement('script')
      Array.from(orig.attributes).forEach((attr) => script.setAttribute(attr.name, attr.value))
      script.textContent = orig.textContent
      document.head.appendChild(script)
    } else {
      document.head.appendChild(node.cloneNode(true))
    }
  })
}
