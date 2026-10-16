/**
 * Landing section order/visibility (extracted from former admin publicSiteDraft helpers).
 */

export type LandingSectionId =
  | 'audioSamples'
  | 'statsBar'
  | 'comparison'
  | 'reviews'
  | 'howItWorks'
  | 'guarantee'
  | 'faq'
  | 'footerCta'

export type SectionVisibility = Record<LandingSectionId, boolean>

export const DEFAULT_SECTION_ORDER: LandingSectionId[] = [
  'audioSamples',
  'statsBar',
  'comparison',
  'reviews',
  'howItWorks',
  'guarantee',
  'faq',
  'footerCta',
]

export const DEFAULT_SECTION_VISIBILITY: SectionVisibility = {
  audioSamples: true,
  statsBar: true,
  comparison: true,
  reviews: true,
  howItWorks: true,
  guarantee: true,
  faq: true,
  footerCta: true,
}

export function resolveSectionVisibility(draft: {
  sectionVisibility?: SectionVisibility
  howItWorksSection?: { hidden?: boolean }
  faqSection?: { hidden?: boolean }
}): SectionVisibility {
  if (draft.sectionVisibility) {
    return { ...DEFAULT_SECTION_VISIBILITY, ...draft.sectionVisibility }
  }
  return {
    ...DEFAULT_SECTION_VISIBILITY,
    howItWorks: !draft.howItWorksSection?.hidden,
    faq: !draft.faqSection?.hidden,
  }
}

export function resolveSectionOrder(stored?: LandingSectionId[]): LandingSectionId[] {
  if (!stored || stored.length === 0) return [...DEFAULT_SECTION_ORDER]
  const known = new Set(DEFAULT_SECTION_ORDER)
  const valid = stored.filter((id) => known.has(id))
  const missing = DEFAULT_SECTION_ORDER.filter((id) => !valid.includes(id))
  return [...valid, ...missing]
}
