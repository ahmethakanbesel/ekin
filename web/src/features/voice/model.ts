import type { Voice, VoiceEngine, VoiceOverride } from "@/api/client"
import { m } from "@/i18n"

/** Mirrors internal/domain/voice/v1. */
export const VOICE_LIMITS = {
  pause: [0, 3],
  seed: [0, 1_000_000],
  lexicon: 500,
  word: 60,
  spoken: 100,
  sample: 400,
} as const

export const QUALITIES = ["fp32", "fp16", "int8"] as const

const number2 = new Intl.NumberFormat(m.locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 })

/** 1 → "1.00". */
export function formatDecimal(n: number): string {
  return number2.format(n)
}

export type VoiceGroup = { key: string; label: string; voices: Voice[] }

/**
 * An engine's voices grouped by accent and gender (American · female, …),
 * in catalogue order. One group needs no heading; callers check length.
 */
export function groupVoices(voices: Voice[]): VoiceGroup[] {
  const groups = new Map<string, VoiceGroup>()
  for (const v of voices) {
    const key = `${v.accent}/${v.gender}`
    let g = groups.get(key)
    if (!g) {
      g = {
        key,
        label: m.voice.group(m.voice.accents[v.accent] ?? v.accent, m.voice.genders[v.gender] ?? v.gender),
        voices: [],
      }
      groups.set(key, g)
    }
    g.voices.push(v)
  }
  return [...groups.values()]
}

/** "Alloy (American female)" → "Alloy", for lists already grouped by accent and gender. */
export function shortLabel(v: Voice): string {
  const i = v.label.indexOf(" (")
  return i > 0 ? v.label.slice(0, i) : v.label
}

export function engineFor(engines: VoiceEngine[] | undefined, language: string) {
  return engines?.find((e) => e.language === language)
}

// ---- Pronunciation dictionary ----

export type LexiconRow = { id: number; written: string; spoken: string }

let nextRow = 0
export function lexiconRow(written = "", spoken = ""): LexiconRow {
  return { id: nextRow++, written, spoken }
}

/** Errors per row id: the written word, the spoken form. */
export type LexiconErrors = Map<number, { written?: string; spoken?: string }>

export function validateLexicon(rows: LexiconRow[]): { rows: LexiconErrors; list?: string } {
  const errors: LexiconErrors = new Map()
  const seen = new Set<string>()
  for (const r of rows) {
    const e: { written?: string; spoken?: string } = {}
    const w = r.written.trim()
    if (!w || /\s/.test(w) || [...w].length > VOICE_LIMITS.word) e.written = m.voice.lexiconWord
    else if (seen.has(w)) e.written = m.voice.lexiconDuplicate
    seen.add(w)
    const sp = r.spoken.trim()
    if (!sp || [...sp].length > VOICE_LIMITS.spoken) e.spoken = m.voice.lexiconSpoken
    if (e.written || e.spoken) errors.set(r.id, e)
  }
  const list = rows.length > VOICE_LIMITS.lexicon ? m.voice.lexiconTooMany(VOICE_LIMITS.lexicon) : undefined
  return { rows: errors, list }
}

export function toLexicon(rows: LexiconRow[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const r of rows) out[r.written.trim()] = r.spoken.trim()
  return out
}

export function fromLexicon(lex: Record<string, string> | undefined): LexiconRow[] {
  return Object.entries(lex ?? {})
    .toSorted(([a], [b]) => a.localeCompare(b, m.locale))
    .map(([w, s]) => lexiconRow(w, s))
}

// ---- Per-pipeline / per-request override ----

/** "" means the workspace default. */
export type VoiceOverrideValues = { voice: string; speed: string }

export function emptyOverride(): VoiceOverrideValues {
  return { voice: "", speed: "" }
}

export function fromOverride(o: VoiceOverride | undefined): VoiceOverrideValues {
  return { voice: o?.voice ?? "", speed: o?.speed ? String(o.speed) : "" }
}

/** The wire override, or undefined when it changes nothing. */
export function toOverride(v: VoiceOverrideValues): VoiceOverride | undefined {
  const speed = v.speed ? Number(v.speed) : undefined
  if (!v.voice && !speed) return undefined
  return { voice: v.voice || undefined, speed: speed || undefined }
}
