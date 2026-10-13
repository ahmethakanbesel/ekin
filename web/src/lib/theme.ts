/** Mirrors the OS colour scheme onto the `.dark` class the theme keys on. */
export function followSystemTheme() {
  const query = window.matchMedia("(prefers-color-scheme: dark)")
  const apply = () => document.documentElement.classList.toggle("dark", query.matches)
  apply()
  query.addEventListener("change", apply)
}
