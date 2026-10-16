import { MoonIcon, SunIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { m } from "@/i18n"
import { setTheme, useTheme } from "@/lib/theme"

/** Flips between light and dark; the choice is stored and beats the OS setting. */
export function ThemeToggle() {
  const theme = useTheme()
  const label = theme === "dark" ? m.theme.toLight : m.theme.toDark
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label={label}
      title={label}
      onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
    >
      {theme === "dark" ? <SunIcon /> : <MoonIcon />}
    </Button>
  )
}
