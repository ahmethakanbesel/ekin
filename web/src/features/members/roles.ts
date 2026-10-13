import type { Role } from "@/api/client"
import { m } from "@/i18n"

export const ROLES: Role[] = ["owner", "editor", "viewer"]

export const roleItems = ROLES.map((r) => ({ value: r, label: m.roles[r] }))

export function isRole(v: unknown): v is Role {
  return typeof v === "string" && (ROLES as string[]).includes(v)
}
