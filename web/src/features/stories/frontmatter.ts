import { m } from "@/i18n"

// Line-based editing of article.md's frontmatter. Only the keys the
// editor shows (title, seoTitle, description, slug, tags) are read and
// rewritten; every other line — other keys, comments, blank lines — is
// kept exactly as the agent wrote it. A known key whose value did not
// change keeps its original lines too, so an edit to the body alone
// leaves the frontmatter byte for byte as it was.

export type ArticleFields = {
  title: string
  seoTitle: string
  description: string
  slug: string
  tags: string[]
}

export const FIELD_KEYS = ["title", "seoTitle", "description", "slug", "tags"] as const
type FieldKey = (typeof FIELD_KEYS)[number]

/** One top-level `key:` of the frontmatter and the lines that belong to it. */
type Entry = { key: string; start: number; end: number }

export type ArticleDoc = {
  /** The frontmatter's lines between the fences; null when there was none. */
  lines: string[] | null
  entries: Entry[]
  /** The known fields as found, to tell which ones changed. */
  fields: ArticleFields
  /** Whether each list key used block style (`- item` lines). */
  blockTags: { indent: string } | null
  /** What sat between the closing fence and the body (its line end and blank lines). */
  lead: string
  body: string
}

const KEY_LINE = /^([A-Za-z0-9_-]+)[ \t]*:(?=[ \t]|$)(.*)$/

/** Browsers' textareas and pasted text may bring \r\n; the server stores \n. */
export const normalizeNewlines = (s: string) => s.replace(/\r\n?/g, "\n")

/**
 * Splits article.md the way the server does (production/v1 ParseArticle):
 * it must start with `---\n`, and the frontmatter ends at the first line
 * that starts with `---`.
 */
export function parseArticle(raw: string): ArticleDoc {
  const text = normalizeNewlines(raw.replace(/^﻿/, ""))
  const empty: ArticleFields = {
    title: "",
    seoTitle: "",
    description: "",
    slug: "",
    tags: [],
  }
  if (!text.startsWith("---\n")) {
    return {
      lines: null,
      entries: [],
      fields: empty,
      blockTags: null,
      lead: "",
      body: text,
    }
  }
  const rest = text.slice(4)
  const end = rest.indexOf("\n---")
  if (end < 0) {
    return {
      lines: null,
      entries: [],
      fields: empty,
      blockTags: null,
      lead: "",
      body: text,
    }
  }
  const lines = rest.slice(0, end).split("\n")
  const tail = rest.slice(end + 4)
  const entries = findEntries(lines)
  const leadMatch = /^[^\n]*\n(?:[ \t]*\n)*/.exec(tail)
  const lead = leadMatch ? leadMatch[0] : tail
  const body = tail.slice(lead.length)

  const fields = { ...empty }
  let blockTags: ArticleDoc["blockTags"] = null
  for (const key of FIELD_KEYS) {
    const entry = entries.find((e) => e.key === key)
    if (!entry) continue
    const entryLines = lines.slice(entry.start, entry.end)
    if (key === "tags") {
      const list = readList(entryLines)
      fields.tags = list.items
      if (list.block) blockTags = { indent: list.block }
    } else {
      fields[key] = readScalar(entryLines)
    }
  }
  return { lines, entries, fields, blockTags, lead, body }
}

/** Puts the fields and body back, rewriting only the known keys that changed. */
export function buildArticle(doc: ArticleDoc, fields: ArticleFields, body: string): string {
  const lines = doc.lines ?? []
  const out: string[] = []
  const replaced = new Set<string>()
  for (let i = 0; i < lines.length; i++) {
    const entry = doc.entries.find((e) => e.start === i)
    if (entry && isField(entry.key) && !replaced.has(entry.key)) {
      replaced.add(entry.key)
      const original = lines.slice(entry.start, entry.end)
      if (sameField(entry.key, doc.fields, fields)) out.push(...original)
      else out.push(...render(entry.key, fields, doc.blockTags))
      i = entry.end - 1
      continue
    }
    out.push(lines[i])
  }
  for (const key of FIELD_KEYS) {
    // A key that was missing is added only once it has a value.
    if (replaced.has(key) || sameField(key, doc.fields, fields)) continue
    out.push(...render(key, fields, null))
  }
  const lead = doc.lines && doc.lead.includes("\n") ? doc.lead : "\n\n"
  const text = normalizeNewlines(body)
  return `---\n${out.join("\n")}\n---${lead}${text.replace(/^\n+/, "")}`
}

const isField = (k: string): k is FieldKey => (FIELD_KEYS as readonly string[]).includes(k)

function sameField(key: FieldKey, a: ArticleFields, b: ArticleFields): boolean {
  if (key === "tags")
    return a.tags.length === b.tags.length && a.tags.every((t, i) => t === b.tags[i])
  return a[key] === b[key]
}

function render(key: FieldKey, f: ArticleFields, block: ArticleDoc["blockTags"]): string[] {
  if (key === "tags") {
    if (block && f.tags.length > 0)
      return ["tags:", ...f.tags.map((t) => `${block.indent}- ${yamlItem(t)}`)]
    return [`tags: [${f.tags.map(yamlItem).join(", ")}]`]
  }
  const value = f[key]
  // An optional key left empty is dropped rather than written as "".
  if (key === "seoTitle" && value.trim() === "") return []
  return [`${key}: ${JSON.stringify(value)}`]
}

/** A tag as a flow-sequence item: plain when YAML reads it back as the same string. */
function yamlItem(t: string): string {
  const plain =
    /^[\p{L}\p{N}][\p{L}\p{N} ._-]*$/u.test(t) &&
    !t.endsWith(" ") &&
    !/^(true|false|yes|no|on|off|null|~)$/i.test(t) &&
    !/^[-+]?(\d|\.\d)/.test(t)
  return plain ? t : JSON.stringify(t)
}

function findEntries(lines: string[]): Entry[] {
  const entries: Entry[] = []
  let i = 0
  while (i < lines.length) {
    const match = KEY_LINE.exec(lines[i])
    if (!match) {
      i++
      continue
    }
    let end = i + 1
    for (let j = i + 1; j < lines.length; j++) {
      const line = lines[j]
      if (/^[ \t]+\S/.test(line) || /^-([ \t]|$)/.test(line)) end = j + 1
      else if (line.trim() === "") continue
      else break
    }
    entries.push({ key: match[1], start: i, end })
    i = end
  }
  return entries
}

/** The text after `key:` on the entry's first line. */
const inlineValue = (lines: string[]) => (KEY_LINE.exec(lines[0])?.[2] ?? "").trim()

function readScalar(lines: string[]): string {
  const inline = inlineValue(lines)
  const more = lines.slice(1)
  const block = /^([|>])[-+0-9]*[ \t]*(#.*)?$/.exec(inline)
  if (block) {
    const content = more.filter((l) => l.trim() !== "")
    const indent = Math.min(...content.map((l) => /^[ \t]*/.exec(l)?.[0].length ?? 0))
    const dedented = more.map((l) => l.slice(Math.min(indent, l.length)))
    return block[1] === "|"
      ? dedented.join("\n").replace(/\n+$/, "")
      : dedented
          .map((l) => l.trim())
          .filter(Boolean)
          .join(" ")
  }
  const joined = [inline, ...more.map((l) => l.trim())].filter(Boolean).join(" ")
  return unquote(joined)
}

function readList(lines: string[]): { items: string[]; block: string | null } {
  const inline = inlineValue(lines)
  if (inline.startsWith("[")) {
    const joined = [inline, ...lines.slice(1).map((l) => l.trim())].join(" ")
    const close = joined.lastIndexOf("]")
    const inner = joined.slice(1, close < 0 ? undefined : close)
    return { items: splitFlow(inner).map(unquote).filter(Boolean), block: null }
  }
  const items = lines
    .slice(1)
    .map((l) => /^([ \t]*)-[ \t]+(.*)$/.exec(l))
    .filter((x): x is RegExpExecArray => !!x)
  if (items.length > 0) {
    return {
      items: items.map((x) => unquote(x[2].trim())).filter(Boolean),
      block: items[0][1],
    }
  }
  const scalar = unquote(inline)
  return {
    items: scalar
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean),
    block: null,
  }
}

/** Splits a flow sequence's inside on commas outside quotes. */
function splitFlow(s: string): string[] {
  const out: string[] = []
  let cur = ""
  let quote: string | null = null
  for (let i = 0; i < s.length; i++) {
    const c = s[i]
    if (quote) {
      cur += c
      if (c === "\\" && quote === '"' && i + 1 < s.length) cur += s[++i]
      else if (c === quote) quote = null
    } else if (c === '"' || c === "'") {
      quote = c
      cur += c
    } else if (c === ",") {
      out.push(cur.trim())
      cur = ""
    } else cur += c
  }
  if (cur.trim()) out.push(cur.trim())
  return out
}

/** A YAML scalar's string: quotes and escapes resolved, a trailing comment dropped. */
function unquote(v: string): string {
  const s = v.trim()
  if (s.startsWith('"')) {
    let i = 1
    while (i < s.length && s[i] !== '"') i += s[i] === "\\" ? 2 : 1
    const inner = s.slice(1, i)
    try {
      return JSON.parse(`"${inner}"`) as string
    } catch {
      return inner.replace(/\\"/g, '"').replace(/\\\\/g, "\\")
    }
  }
  if (s.startsWith("'")) {
    let i = 1
    while (i < s.length) {
      if (s[i] === "'" && s[i + 1] === "'") i += 2
      else if (s[i] === "'") break
      else i++
    }
    return s.slice(1, i).replace(/''/g, "'")
  }
  return s.replace(/[ \t]+#.*$/, "").trim()
}

/** Words as the server counts them (strings.Fields). */
export const countWords = (s: string) => s.split(/\s+/).filter(Boolean).length

/** Characters as the server counts them (runes). */
export const countChars = (s: string) => [...s].length

export const SLUG = /^[a-z0-9]+(-[a-z0-9]+)*$/

/** Mirrors production/v1 ParseArticle; the first problem per field. */
export function articleErrors(
  f: ArticleFields,
  body: string
): Partial<Record<FieldKey | "body", string>> {
  const t = m.editor.article.errors
  const errors: Partial<Record<FieldKey | "body", string>> = {}
  if (!f.title.trim()) errors.title = t.titleRequired
  const d = countChars(f.description)
  if (d < 50 || d > 200) errors.description = t.description(d)
  if (!SLUG.test(f.slug)) errors.slug = t.slug
  if (countChars(f.seoTitle) > 60) errors.seoTitle = t.seoTitle
  if (f.tags.length === 0) errors.tags = t.tags
  const b = body.trim()
  if (b.startsWith("# ")) errors.body = t.h1
  else if (countWords(b) < 100) errors.body = t.short
  return errors
}
