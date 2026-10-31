import type { SiteSeverity } from "@/api/client"
import { m } from "@/i18n"
import { badgeTone } from "@/lib/tones"

/** A rule's title and why it matters, in plain words. */
export function ruleText(rule: string): { title: string; why: string } {
  return m.site.rules[rule] ?? m.site.unknownRule(rule)
}

export const SEVERITIES: SiteSeverity[] = ["error", "warning", "notice"]

/** Rules about the site as a whole, raised on its address (site.Audit). */
export const SITE_WIDE = new Set([
  "robots_blocks_ai",
  "llms_txt_missing",
  "sitemap_missing",
])

export const severityBadge: Record<SiteSeverity, string> = {
  error: badgeTone.danger,
  warning: badgeTone.attention,
  notice: badgeTone.muted,
}

/** The path of a URL on the site, for compact lists; the whole URL elsewhere. */
export function shortURL(url: string, root: string | undefined): string {
  if (root && url.startsWith(root)) return "/" + url.slice(root.length)
  try {
    const u = new URL(url)
    return u.pathname + u.search
  } catch {
    return url
  }
}
