import { createContext, useContext } from "react"

import type { Workspace } from "@/api/client"

export const WorkspaceContext = createContext<Workspace | null>(null)

/** The workspace selected in the URL. Only valid under /w/$workspaceId. */
export function useWorkspace(): Workspace {
  const ws = useContext(WorkspaceContext)
  if (!ws) throw new Error("useWorkspace outside a workspace route")
  return ws
}

const LAST_WORKSPACE = "kurgu.lastWorkspace"

export function rememberWorkspace(id: string) {
  try {
    localStorage.setItem(LAST_WORKSPACE, id)
  } catch {
    // Storage may be unavailable (private mode); remembering is a nicety.
  }
}

export function lastWorkspace(): string | null {
  try {
    return localStorage.getItem(LAST_WORKSPACE)
  } catch {
    return null
  }
}
