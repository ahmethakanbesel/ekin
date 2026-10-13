import { m } from "@/i18n"

export function workspaceNameError(name: string): string | undefined {
  const t = name.trim()
  if (!t) return m.workspace.nameRequired
  if ([...t].length > 80) return m.workspace.nameTooLong
  return undefined
}
