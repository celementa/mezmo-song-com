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

// Auto-discover all template files in this directory (index.ts and types.ts
// are not .tsx). На mezmo-song.com шаблонов два: gold (дизайн из настроек) и
// classic (фолбэк getTemplate).
const modules = import.meta.glob<{ default: TemplateDef }>('./*.tsx', { eager: true })

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

export type { TemplateDef } from './types'
