import { ApiError } from "@/api/client"
import { m } from "@/i18n"

// Codes that mean the same thing wherever they come from get our own
// wording; anything else shows the server's message.
const known: Record<string, string> = {
  Unauthenticated: m.errors.sessionEnded,
  Forbidden: m.errors.forbidden,
  LastOwner: m.errors.lastOwner,
  TooManyAttempts: m.errors.tooManyAttempts,
  AlreadyMember: m.errors.alreadyMember,
  EmailTaken: m.errors.emailTaken,
}

/** The text to show for a failed request: the server's own message when
 * there is one, otherwise a generic line. */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === "NetworkError") return m.errors.network
    return known[err.code] ?? (err.message || m.errors.unknown)
  }
  if (err instanceof Error && err.message) return err.message
  return m.errors.unknown
}
