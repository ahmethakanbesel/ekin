import { createContext, useContext } from "react"

import type { Role, Workspace } from "@/api/client"

export const WorkspaceContext = createContext<Workspace | null>(null)

/** The workspace selected in the URL. Only valid under /w/$workspaceId. */
export function useWorkspace(): Workspace {
  const ws = useContext(WorkspaceContext)
  if (!ws) throw new Error("useWorkspace outside a workspace route")
  return ws
}

/**
 * What a role allows beyond reading. edit: pipelines, runs and job
 * cancellation. manage: also workspace settings, members and invitations.
 * The server enforces the same rules; this only decides what to offer.
 */
export type Permission = "edit" | "manage"

const rank: Record<Role, number> = { viewer: 0, editor: 1, owner: 2 }
const needed: Record<Permission, number> = { edit: 1, manage: 2 }

export function can(role: Role, permission: Permission): boolean {
  return rank[role] >= needed[permission]
}

/** Whether the caller may do `permission` in the current workspace. */
export function useCan(permission: Permission): boolean {
  return can(useWorkspace().role, permission)
}
