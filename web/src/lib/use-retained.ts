import { useState } from "react"

/**
 * Returns value, or the last non-null value once it becomes null — so a
 * dialog driven by "the item being acted on" keeps its text while it
 * animates closed.
 */
export function useRetained<T>(value: T | null): T | null {
  const [kept, setKept] = useState(value)
  if (value !== null && value !== kept) setKept(value)
  return value ?? kept
}
