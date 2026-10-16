import { useSyncExternalStore } from "react"

export type Theme = "light" | "dark"

// A choice made with the theme toggle wins over the OS setting. Storage can
// be unavailable (private mode, blocked site data); then the choice lasts
// for this page only and the OS setting applies on the next visit.
const STORAGE_KEY = "kurgu:theme"
const listeners = new Set<() => void>()
let chosen: Theme | null = readChoice()

function readChoice(): Theme | null {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    return raw === "light" || raw === "dark" ? raw : null
  } catch {
    return null
  }
}

const systemQuery = () => window.matchMedia("(prefers-color-scheme: dark)")

function currentTheme(): Theme {
  return chosen ?? (systemQuery().matches ? "dark" : "light")
}

function apply() {
  document.documentElement.classList.toggle("dark", currentTheme() === "dark")
  for (const l of listeners) l()
}

/** Puts the stored choice, else the OS colour scheme, on the `.dark` class the theme keys on. */
export function initTheme() {
  apply()
  systemQuery().addEventListener("change", apply)
}

/** Stores a light or dark choice that overrides the OS setting from now on. */
export function setTheme(theme: Theme) {
  chosen = theme
  try {
    window.localStorage.setItem(STORAGE_KEY, theme)
  } catch {
    // remembered for this page only
  }
  apply()
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/** The theme in effect, re-rendering when the toggle or the OS changes it. */
export function useTheme(): Theme {
  return useSyncExternalStore(subscribe, currentTheme)
}
