import type { LandingData } from '@/hooks/useLandingData'

/**
 * A template definition. Drop a file exporting this as `default` into the
 * `templates/` folder and it will be auto-discovered.
 *
 * Templates receive fully pre-processed data via `LandingData` — no need to
 * handle API calls, state management, or data transformation. Just render.
 */
export type TemplateDef = {
  /** Unique identifier — must match the filename (e.g. "classic" for classic.tsx). */
  id: string

  /** Human-readable name shown in the admin template picker. */
  name: string

  /** Optional preview thumbnail URL for the admin UI. */
  thumbnail?: string

  /** The React component that renders the landing page. */
  Render: React.ComponentType<LandingData>
}
