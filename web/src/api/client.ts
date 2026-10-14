import createClient from "openapi-fetch"

import type { components, paths } from "./schema"

// The generated paths omit the spec's `servers.url`, so the base URL
// carries it. The UI is served by the same Go binary as the API (and by
// the Vite proxy in dev), so a relative base is enough.
export const API_BASE = "/api/v1"

export const api = createClient<paths>({
  baseUrl: API_BASE,
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
export type Document = Schemas["Document"]
export type DocumentFormat = Document["format"]
export type DocumentStatus = Document["status"]
export type DocumentPage = Schemas["DocumentPage"]

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
  if (!response.ok) throw toApiError(response.status, error, response.statusText)
  return data as T
}

/** The ApiError for a non-2xx answer whose body parsed to `body`. */
export function toApiError(status: number, body: unknown, statusText = ""): ApiError {
  if (isErrorBody(body)) return new ApiError(status, body.code, body.message)
  const text = typeof body === "string" && body.trim() ? body.trim() : statusText
  return new ApiError(status, "HttpError", text || `HTTP ${status}`)
}

function isErrorBody(v: unknown): v is Schemas["Error"] {
  if (typeof v !== "object" || v === null) return false
  const r = v as Record<string, unknown>
  return typeof r.code === "string" && typeof r.message === "string"
}

export function isApiError(err: unknown, code?: string): err is ApiError {
  return err instanceof ApiError && (code === undefined || err.code === code)
}
