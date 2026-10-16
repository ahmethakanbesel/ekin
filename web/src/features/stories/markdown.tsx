import { useMemo } from "react"
import DOMPurify from "dompurify"
import { marked } from "marked"

import { cn } from "@/lib/utils"

// Links in agent-written Markdown open in a new tab and pass no referrer.
DOMPurify.addHook("afterSanitizeAttributes", (node) => {
  if (node.tagName === "A" && node.getAttribute("href")) {
    node.setAttribute("target", "_blank")
    node.setAttribute("rel", "noopener noreferrer")
  }
})

/** Markdown from the agent, rendered and sanitised. */
export function Markdown({
  source,
  className,
  lang,
}: {
  source: string
  className?: string
  /** The content's language, when it is not the UI's. */
  lang?: string
}) {
  const html = useMemo(
    () => DOMPurify.sanitize(marked.parse(source, { async: false, gfm: true })),
    [source]
  )
  return (
    <div lang={lang} className={cn("markdown", className)} dangerouslySetInnerHTML={{ __html: html }} />
  )
}

export type Frontmatter = Record<string, string | string[]>

/**
 * Splits a leading YAML frontmatter block off a Markdown document. Only
 * the flat shapes the agent writes are understood: `key: value`, quoted
 * values, `key: [a, b]` and `key:` followed by `- item` lines.
 */
export function splitFrontmatter(text: string): { data: Frontmatter; body: string } {
  const src = text.replace(/^﻿/, "")
  const match = /^---[ \t]*\r?\n([\s\S]*?)\r?\n---[ \t]*(?:\r?\n|$)/.exec(src)
  if (!match) return { data: {}, body: src }
  const data: Frontmatter = {}
  let listKey: string | undefined
  for (const raw of match[1].split(/\r?\n/)) {
    const line = raw.replace(/\s+$/, "")
    if (!line.trim() || line.trim().startsWith("#")) continue
    const item = /^\s*-\s+(.*)$/.exec(line)
    if (item && listKey) {
      const list = data[listKey]
      const value = unquote(item[1])
      data[listKey] = Array.isArray(list) ? [...list, value] : [value]
      continue
    }
    const pair = /^([A-Za-z0-9_-]+)\s*:\s*(.*)$/.exec(line)
    if (!pair) continue
    const [, key, value] = pair
    listKey = undefined
    if (value === "") {
      listKey = key
      data[key] = []
    } else if (value.startsWith("[") && value.endsWith("]")) {
      data[key] = value
        .slice(1, -1)
        .split(",")
        .map((v) => unquote(v.trim()))
        .filter(Boolean)
    } else {
      data[key] = unquote(value)
    }
  }
  return { data, body: src.slice(match[0].length) }
}

function unquote(v: string): string {
  if (v.length >= 2 && ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'")))) {
    return v.slice(1, -1).replace(/\\"/g, '"')
  }
  return v
}

/** A frontmatter value as text (lists joined). */
export function fmText(data: Frontmatter, key: string): string | undefined {
  const v = data[key]
  if (v === undefined) return undefined
  const s = Array.isArray(v) ? v.join(", ") : v
  return s || undefined
}

/** A frontmatter value as a list (a comma-separated string split). */
export function fmList(data: Frontmatter, key: string): string[] {
  const v = data[key]
  if (v === undefined) return []
  if (Array.isArray(v)) return v
  return v
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)
}
