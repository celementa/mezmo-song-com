/**
 * Template auto-discovery registry.
 *
 * Drop a .tsx file in this folder that `export default`s a `TemplateDef` and
 * it will be automatically registered — no imports or config to update.
 *
 * The registry uses Vite's `import.meta.glob` for eager loading of template
 * metadata. Template rendering components are loaded on first use.
 */
import type { TemplateDef } from './types'

// Auto-discover all template files in this directory.
// Excludes index.ts and types.ts via negative glob patterns.
// marketplace.tsx и grace.tsx исключены намеренно: оба используются редко и иначе
// попадали бы мёртвым кодом в стартовый бандл главной и раздували index.js → били по LCP.
// marketplace.tsx используется в отдельной админ-фиче, grace.tsx — только на /old-design.
// Их модули загружаются лениво (lazy) на нужных маршрутах. Чтобы вернуть их в eager-load —
// убери соответствующие '!./...' из паттерна.
const modules = import.meta.glob<{ default: TemplateDef }>(
  ['./*.tsx', '!./types.ts', '!./_scaffold.tsx', '!./marketplace.tsx', '!./grace.tsx'],
  { eager: true },
)

export const templates: Record<string, TemplateDef> = {}

for (const [, mod] of Object.entries(modules)) {
  const def = mod?.default
  if (def?.id && def?.Render) {
    templates[def.id] = def
  }
}

/** Get a template by ID, falling back to "classic". */
export function getTemplate(id?: string | null): TemplateDef {
  if (id && templates[id]) return templates[id]
  return templates['classic']
}

/** List all available templates (for admin UI). */
export function listTemplates(): TemplateDef[] {
  return Object.values(templates)
}

/** Raw source of the scaffold template for download. */
export const scaffoldSource = () =>
  import('./_scaffold.tsx?raw').then(m => m.default)

export type { TemplateDef } from './types'
