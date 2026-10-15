import type { DeliverableKind, Template, TemplateFont, TemplateInput } from "@/api/client"
import { m } from "@/i18n"

// ---- Fonts ----

/** The families the composition kit bundles (see ./fonts.css). */
export const FONTS: TemplateFont[] = [
  "Inter",
  "Lora",
  "Nunito",
  "Oswald",
  "Caveat",
  "JetBrains Mono",
  "Fraunces",
  "Instrument Serif",
]

const generic: Record<TemplateFont, string> = {
  Inter: "sans-serif",
  Lora: "serif",
  Nunito: "sans-serif",
  Oswald: "sans-serif",
  Caveat: "cursive",
  "JetBrains Mono": "monospace",
  Fraunces: "serif",
  "Instrument Serif": "serif",
}

/** The weight a render sets titles in — the heaviest the kit ships. */
export const HEADING_WEIGHT: Record<TemplateFont, number> = {
  Inter: 800,
  Lora: 700,
  Nunito: 800,
  Oswald: 600,
  Caveat: 600,
  "JetBrains Mono": 500,
  Fraunces: 700,
  "Instrument Serif": 400,
}

/** The weight for running text — the lightest the kit ships. */
export const BODY_WEIGHT: Record<TemplateFont, number> = {
  Inter: 400,
  Lora: 400,
  Nunito: 400,
  Oswald: 600,
  Caveat: 600,
  "JetBrains Mono": 500,
  Fraunces: 400,
  "Instrument Serif": 400,
}

export function fontStack(font: TemplateFont): string {
  return `"${font}", ${generic[font] ?? "sans-serif"}`
}

export function headingStyle(font: TemplateFont) {
  return { fontFamily: fontStack(font), fontWeight: HEADING_WEIGHT[font] ?? 700 }
}

export function bodyStyle(font: TemplateFont) {
  return { fontFamily: fontStack(font), fontWeight: BODY_WEIGHT[font] ?? 400 }
}

// ---- Colours ----

export const HEX = /^#[0-9A-Fa-f]{6}$/

function channel(v: number): number {
  const c = v / 255
  return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
}

/** WCAG relative luminance of a #RRGGBB colour. */
export function luminance(hex: string): number {
  const n = Number.parseInt(hex.slice(1), 16)
  return 0.2126 * channel((n >> 16) & 255) + 0.7152 * channel((n >> 8) & 255) + 0.0722 * channel(n & 255)
}

/** WCAG contrast ratio, 1–21. */
export function contrast(a: string, b: string): number {
  const la = luminance(a)
  const lb = luminance(b)
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
}

/** Below this, a palette colour is not legible enough for a title. */
const MIN_TITLE_CONTRAST = 3

const NEUTRAL = { background: "#F4F4F5", text: "#18181B" }

export type TitleColours = { background: string; text: string; accent?: string }

/**
 * The colours of a template's title card: the first palette colour as
 * background, the palette colour that contrasts most with it as text
 * (black or white when none is legible) and, when one stands out enough,
 * another as accent.
 */
export function titleColours(palette: string[]): TitleColours {
  const colours = palette.filter((c) => HEX.test(c))
  if (colours.length === 0) return NEUTRAL
  const background = colours[0]
  const rest = colours.slice(1)
  let text = rest.reduce<string | undefined>(
    (best, c) => (!best || contrast(c, background) > contrast(best, background) ? c : best),
    undefined
  )
  if (!text || contrast(text, background) < MIN_TITLE_CONTRAST) {
    text = contrast("#FFFFFF", background) >= contrast("#111111", background) ? "#FFFFFF" : "#111111"
  }
  const accent = rest.find((c) => c !== text && contrast(c, background) >= 1.6)
  return { background, text, accent }
}

// ---- Kinds and sources ----

export const KINDS: DeliverableKind[] = ["video", "article"]

export const TEMPLATE_TABS = ["all", "video", "article"] as const
export type TemplateTab = (typeof TEMPLATE_TABS)[number]

export function isEditable(t: Pick<Template, "source">): boolean {
  return t.source !== "builtin"
}

/** Templates offered for outputs: those for at least one of the kinds. */
export function fitsKinds(t: Pick<Template, "kinds">, kinds: DeliverableKind[]): boolean {
  return kinds.some((k) => t.kinds.includes(k))
}

// ---- Form ----

export const TEMPLATE_LIMITS = {
  name: 80,
  description: 500,
  inspiration: 300,
  musicMood: 300,
  guide: 20000,
  palette: 8,
  channel: 300,
  notes: 2000,
} as const

export type TemplateFormValues = {
  name: string
  description: string
  inspiration: string
  kinds: DeliverableKind[]
  palette: string[]
  headingFont: TemplateFont
  bodyFont: TemplateFont
  musicMood: string
  guide: string
}

export function emptyTemplateValues(): TemplateFormValues {
  return {
    name: "",
    description: "",
    inspiration: "",
    kinds: ["video"],
    palette: ["#0F1B2D", "#F3EDE4", "#E8A87C"],
    headingFont: "Inter",
    bodyFont: "Inter",
    musicMood: "",
    guide: m.templates.form.starterGuide,
  }
}

export function fromTemplate(t: Template): TemplateFormValues {
  return {
    name: t.name,
    description: t.description,
    inspiration: t.inspiration,
    kinds: [...t.kinds],
    palette: [...t.palette],
    headingFont: t.headingFont,
    bodyFont: t.bodyFont,
    musicMood: t.musicMood,
    guide: t.guide,
  }
}

/** The wire input. Call only on values that validate. */
export function toTemplateInput(v: TemplateFormValues): TemplateInput {
  return {
    name: v.name.trim(),
    description: v.description.trim(),
    inspiration: v.inspiration.trim(),
    kinds: KINDS.filter((k) => v.kinds.includes(k)),
    palette: v.palette.map((c) => c.toUpperCase()),
    headingFont: v.headingFont,
    bodyFont: v.bodyFont,
    musicMood: v.musicMood.trim(),
    guide: v.guide.trim(),
  }
}

const length = (s: string) => [...s].length

/** Mirrors internal/domain/template/v1 (Template.apply). Keys are form field names. */
export function validateTemplate(v: TemplateFormValues): Partial<Record<string, string>> {
  const t = m.templates.form.validation
  const fields: Partial<Record<string, string>> = {}
  const name = v.name.trim()
  if (!name) fields.name = t.nameRequired
  else if (length(name) > TEMPLATE_LIMITS.name) fields.name = t.nameTooLong
  if (length(v.description.trim()) > TEMPLATE_LIMITS.description) {
    fields.description = t.descriptionTooLong
  }
  if (length(v.inspiration.trim()) > TEMPLATE_LIMITS.inspiration) {
    fields.inspiration = t.inspirationTooLong
  }
  if (length(v.musicMood.trim()) > TEMPLATE_LIMITS.musicMood) fields.musicMood = t.musicMoodTooLong
  if (v.kinds.length === 0) fields.kinds = t.kindsRequired
  const guide = v.guide.trim()
  if (!guide) fields.guide = t.guideRequired
  else if (length(guide) > TEMPLATE_LIMITS.guide) fields.guide = t.guideTooLong
  if (v.palette.length > TEMPLATE_LIMITS.palette) fields.palette = t.paletteTooMany
  v.palette.forEach((c, i) => {
    if (!HEX.test(c)) fields[`palette[${i}]`] = t.hex
  })
  return fields
}
