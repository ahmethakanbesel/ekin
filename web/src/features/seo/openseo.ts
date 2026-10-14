import type { OpenSEOProject } from "@/api/client"

// Mirrors of the server's direct-refresh rules (domain/seo/v1 Profile),
// so the settings can show what a refresh will spend while it is edited.
// The server's estimatedCredits is authoritative once saved.

export const MAX_SEEDS = 5
export const MAX_COMPETITORS = 3
export const DEFAULT_MAX_DIFFICULTY = 50
const CREDITS_PER_SEED = 54
const CREDITS_PER_COMPETITOR = 300
/** Refreshes estimated above this ask for confirmation first. */
export const CONFIRM_CREDITS = 500

/** The seeds a refresh researches: the seeds, else up to five topics (split on , ; and lines). */
export function seedList(seeds: string[], topics: string): string[] {
  const src = seeds.length > 0 ? seeds : topics.split(/[,;\n]/)
  const out: string[] = []
  const seen = new Set<string>()
  for (const raw of src) {
    const s = raw.trim()
    if (!s || seen.has(s.toLowerCase()) || out.length === MAX_SEEDS) continue
    seen.add(s.toLowerCase())
    out.push(s)
  }
  return out
}

/** Profile.EstimatedCredits: seeds × 54 + competitors × 300. */
export function estimateCredits(seeds: string[], topics: string, competitors: string[]): number {
  return (
    seedList(seeds, topics).length * CREDITS_PER_SEED + competitors.length * CREDITS_PER_COMPETITOR
  )
}

/** A competitor as the server stores it: no scheme, no trailing slash. */
export function normalizeDomain(raw: string): string {
  return raw
    .trim()
    .toLowerCase()
    .replace(/^https?:\/\//, "")
    .replace(/\/+$/, "")
}

export const isDomain = (s: string) => s.includes(".") && !/[\s/]/.test(s)

/** DataForSEO location codes of common countries (adapter/openseo Locations). */
const LOCATIONS: Record<string, number> = {
  TR: 2792,
  US: 2840,
  GB: 2826,
  DE: 2276,
  FR: 2250,
  NL: 2528,
  CA: 2124,
  AU: 2036,
  IE: 2372,
  AZ: 2031,
  CY: 2196,
  AT: 2040,
  CH: 2756,
  BE: 2056,
  SE: 2752,
  ES: 2724,
  IT: 2380,
}

/** The two-letter country of a location code, when it is a known one. */
export function countryOf(locationCode: number): string | undefined {
  return Object.entries(LOCATIONS).find(([, code]) => code === locationCode)?.[0]
}

/** "TR · tr", or the raw location code when the country is not known. */
export function marketLabel(p: Pick<OpenSEOProject, "locationCode" | "languageCode">): string {
  const country = countryOf(p.locationCode) ?? `#${p.locationCode}`
  return p.languageCode ? `${country} · ${p.languageCode}` : country
}
