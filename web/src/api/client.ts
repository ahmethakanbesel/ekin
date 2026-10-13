import createClient from "openapi-fetch"

import type { components, paths } from "./schema"

// The generated paths omit the spec's `servers.url`, so the base URL
// carries it. The UI is served by the same Go binary as the API (and by
// the Vite proxy in dev), so a relative base is enough.
export const api = createClient<paths>({
  baseUrl: "/api/v1",
  // Looked up per call so a fetch installed later (tests, polyfills) is
  // picked up.
  fetch: (...args) => globalThis.fetch(...args),
})

export type Schemas = components["schemas"]
export type Workspace = Schemas["Workspace"]
export type Pipeline = Schemas["Pipeline"]
export type PipelineDefinition = Schemas["PipelineDefinition"]
export type Language = Schemas["Language"]
export type Job = Schemas["Job"]
export type JobStatus = Schemas["JobStatus"]
export type User = Schemas["User"]
export type Role = Schemas["Role"]
export type Member = Schemas["Member"]
export type Invitation = Schemas["Invitation"]
export type InvitationPreview = Schemas["InvitationPreview"]

/** A failed API call: the server's `{code, message}` plus the HTTP status. */
export class ApiError extends Error {
  readonly code: string
  readonly status: number

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = "ApiError"
    this.code = code
    this.status = status
  }
}

type Result<T> = {
  data?: T
  error?: unknown
  response: Response
}

/**
 * Resolves an openapi-fetch call to its data, throwing ApiError on any
 * non-2xx answer. Bodies that are not the `{code, message}` shape (a
 * proxy's 502 page, say) still become an ApiError with a generic code.
 */
export async function call<T>(request: Promise<Result<T>>): Promise<T> {
  let result: Result<T>
  try {
    result = await request
  } catch (cause) {
    throw new ApiError(0, "NetworkError", cause instanceof Error ? cause.message : String(cause))
  }
  const { data, error, response } = result
  if (!response.ok) {
    if (isErrorBody(error)) throw new ApiError(response.status, error.code, error.message)
    const text = typeof error === "string" && error.trim() ? error.trim() : response.statusText
    throw new ApiError(response.status, "HttpError", text || `HTTP ${response.status}`)
  }
  return data as T
}

function isErrorBody(v: unknown): v is Schemas["Error"] {
  if (typeof v !== "object" || v === null) return false
  const r = v as Record<string, unknown>
  return typeof r.code === "string" && typeof r.message === "string"
}

export function isApiError(err: unknown, code?: string): err is ApiError {
  return err instanceof ApiError && (code === undefined || err.code === code)
}
