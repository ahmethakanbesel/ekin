import { m } from "@/i18n"

export const PASSWORD_MIN = 10
export const PASSWORD_MAX = 128

export function personNameError(name: string): string | undefined {
  const t = name.trim()
  if (!t) return m.account.nameRequired
  if ([...t].length > 80) return m.account.nameTooLong
  return undefined
}

/** For a password being chosen; an existing one is only checked for presence. */
export function newPasswordError(password: string): string | undefined {
  if ([...password].length < PASSWORD_MIN) return m.account.passwordTooShort
  if ([...password].length > PASSWORD_MAX) return m.account.passwordTooLong
  return undefined
}

export function confirmError(password: string, confirm: string): string | undefined {
  return password === confirm ? undefined : m.account.passwordMismatch
}
