import { ApiError } from "@/api/client"
import { m } from "@/i18n"

/** The text to show for a failed request: the server's own message when
 * there is one, otherwise a generic line. */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === "NetworkError") return m.errors.network
    return err.message || m.errors.unknown
  }
  if (err instanceof Error && err.message) return err.message
  return m.errors.unknown
}
