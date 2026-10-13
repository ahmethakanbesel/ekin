export type LoginSearch = { next?: string }

/** `?next=/w/…/jobs` — where to go after signing in. */
export function validateLoginSearch(search: Record<string, unknown>): LoginSearch {
  return typeof search.next === "string" && search.next ? { next: search.next } : {}
}

/**
 * The path to continue to after signing in: only a same-site path
 * ("/…", not "//host" or "/\host", which browsers read as another
 * origin), and never the sign-in page itself. Anything else goes home.
 */
export function safeNext(next: string | undefined): string {
  if (!next || !next.startsWith("/")) return "/"
  if (next.startsWith("//") || next.startsWith("/\\")) return "/"
  if (next === "/login" || next.startsWith("/login?") || next.startsWith("/login/")) return "/"
  return next
}

/** Pages that work without a session; a 401 there is not a lost session. */
export function isPublicPath(pathname: string): boolean {
  return pathname === "/login" || pathname.startsWith("/invite/")
}
