import type { OpportunityInput } from "@/api/client"

export const KEYWORD_MAX = 120

export type ParsedKeywords = {
  items: Omit<OpportunityInput, "language">[]
  /** Lines that had no usable keyword (or one that is too long). */
  skipped: number
  /** The columns read from a header row, when there was one. */
  columns: Column[] | null
}

export type Column = "keyword" | "volume" | "difficulty" | "intent"

// Header cells, lower-cased, that name a column. OpenSEO exports use
// "Keyword", "Search Volume", "Keyword Difficulty", "Intent"; other tools
// use "Query", "Volume", "KD" and the like.
const HEADER_MATCHERS: Array<[Column, RegExp]> = [
  ["difficulty", /difficulty|^kd\b|^kd%?$|zorluk/],
  ["volume", /volume|searches|^sv$|hacim/],
  ["intent", /intent|niyet/],
  ["keyword", /keyword|query|term|phrase|anahtar/],
]

/** Splits one line into cells, honouring double quotes ("a, b" and ""). */
function splitLine(line: string, delimiter: string): string[] {
  const cells: string[] = []
  let cell = ""
  let quoted = false
  for (let i = 0; i < line.length; i++) {
    const ch = line[i]
    if (quoted) {
      if (ch === '"' && line[i + 1] === '"') {
        cell += '"'
        i++
      } else if (ch === '"') quoted = false
      else cell += ch
    } else if (ch === '"' && cell.trim() === "") {
      quoted = true
      cell = ""
    } else if (ch === delimiter) {
      cells.push(cell.trim())
      cell = ""
    } else cell += ch
  }
  cells.push(cell.trim())
  return cells
}

/** Tabs (a spreadsheet paste), then semicolons (European CSV), then commas. */
function pickDelimiter(lines: string[]): string {
  const sample = lines.slice(0, 5).join("\n")
  if (sample.includes("\t")) return "\t"
  if (sample.includes(";") && !sample.includes(",")) return ";"
  return ","
}

function headerColumns(cells: string[]): Map<Column, number> | null {
  const found = new Map<Column, number>()
  cells.forEach((raw, i) => {
    const cell = raw.toLowerCase().trim()
    for (const [column, re] of HEADER_MATCHERS) {
      if (re.test(cell)) {
        if (!found.has(column)) found.set(column, i)
        break
      }
    }
  })
  if (!found.has("keyword")) return null
  // "keyword research tips" alone is a keyword, not a header: a header
  // names a second column too, or is just the column name.
  const keywordCell = cells[found.get("keyword")!].toLowerCase().trim()
  return found.size >= 2 || STRICT_KEYWORD_HEADER.test(keywordCell) ? found : null
}

const STRICT_KEYWORD_HEADER =
  /^(keywords?|quer(y|ies)|search terms?|terms?|phrases?|anahtar kelime(ler)?)$/

/** "1,900", "1.900", "1 900", "1.9k", "12K" → a whole number; else undefined. */
export function parseVolume(raw: string | undefined): number | undefined {
  if (!raw) return undefined
  const s = raw.trim().replace(/\s/g, "")
  const short = /^(\d+(?:[.,]\d+)?)([km])$/i.exec(s)
  if (short) {
    const n = Number(short[1].replace(",", "."))
    return Math.round(n * (short[2].toLowerCase() === "k" ? 1_000 : 1_000_000))
  }
  if (!/^\d[\d.,]*$/.test(s)) return undefined
  return Number(s.replace(/[.,]/g, ""))
}

/** "34", "34.5", "34%" → 0–100; blanks, "-" and "n/a" → undefined (unknown). */
export function parseDifficulty(raw: string | undefined): number | undefined {
  if (!raw) return undefined
  const s = raw.trim().replace("%", "").replace(",", ".")
  if (!/^\d+(\.\d+)?$/.test(s)) return undefined
  return Math.min(100, Math.max(0, Math.round(Number(s))))
}

/**
 * Reads pasted keywords leniently: one per line, optionally
 * `keyword, volume, difficulty`, or a CSV/TSV whose header row names the
 * keyword, volume, difficulty and intent columns. Repeated keywords are
 * kept once.
 */
export function parseKeywords(text: string): ParsedKeywords {
  const lines = text.split(/\r?\n/).filter((l) => l.trim() !== "")
  if (lines.length === 0) return { items: [], skipped: 0, columns: null }

  const delimiter = pickDelimiter(lines)
  const header = headerColumns(splitLine(lines[0].replace(/^﻿/, ""), delimiter))
  const cols: Map<Column, number> =
    header ??
    new Map<Column, number>([
      ["keyword", 0],
      ["volume", 1],
      ["difficulty", 2],
    ])
  const rows = header ? lines.slice(1) : lines

  const seen = new Set<string>()
  const items: ParsedKeywords["items"] = []
  let skipped = 0
  for (const line of rows) {
    const cells = splitLine(line, delimiter)
    const at = (c: Column) => {
      const i = cols.get(c)
      return i === undefined ? undefined : cells[i]
    }
    const keyword = (at("keyword") ?? "").replace(/\s+/g, " ").trim()
    if (!keyword || [...keyword].length > KEYWORD_MAX) {
      skipped++
      continue
    }
    const key = keyword.toLocaleLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    const item: ParsedKeywords["items"][number] = { keyword }
    const volume = parseVolume(at("volume"))
    if (volume !== undefined) item.volume = volume
    const difficulty = parseDifficulty(at("difficulty"))
    if (difficulty !== undefined) item.difficulty = difficulty
    const intent = at("intent")?.trim()
    if (intent) item.intent = intent.toLowerCase()
    items.push(item)
  }
  return {
    items,
    skipped,
    columns: header ? [...header.keys()].toSorted((a, b) => header.get(a)! - header.get(b)!) : null,
  }
}
