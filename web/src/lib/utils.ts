import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * A free-text search parameter (`?q=`), trimmed; undefined when empty.
 * The router parses JSON-looking values, so `?q=2024` arrives as a number.
 */
export function parseSearchText(raw: unknown): string | undefined {
  const text = typeof raw === "string" || typeof raw === "number" ? String(raw).trim() : ""
  return text || undefined
}
