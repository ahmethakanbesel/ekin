import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { useNavigate, useRouterState } from "@tanstack/react-router"
import { ChevronsUpDownIcon, LayoutGridIcon, PlusIcon } from "lucide-react"

import type { Workspace } from "@/api/client"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { m } from "@/i18n"
import { workspacesQuery } from "@/queries/workspaces"
import { CreateWorkspaceDialog } from "./create-workspace-dialog"

const sectionRoutes = {
  stories: "/w/$workspaceId/stories",
  pipelines: "/w/$workspaceId/pipelines",
  knowledge: "/w/$workspaceId/knowledge",
  jobs: "/w/$workspaceId/jobs",
  members: "/w/$workspaceId/members",
  settings: "/w/$workspaceId/settings",
} as const

/** The section of the current workspace URL, kept when switching. */
function currentSection(pathname: string): keyof typeof sectionRoutes {
  const part = pathname.split("/")[3]
  return part !== undefined && Object.hasOwn(sectionRoutes, part)
    ? (part as keyof typeof sectionRoutes)
    : "stories"
}

export function WorkspaceSwitcher({ current }: { current: Workspace }) {
  const { data: workspaces } = useQuery(workspacesQuery())
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const navigate = useNavigate()
  const [creating, setCreating] = useState(false)

  const go = (workspaceId: string) => {
    if (workspaceId === current.id) return
    void navigate({ to: sectionRoutes[currentSection(pathname)], params: { workspaceId } })
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          className="flex w-full items-center gap-2.5 rounded-2xl px-2.5 py-2 text-left outline-none hover:bg-sidebar-accent focus-visible:ring-3 focus-visible:ring-ring/30 aria-expanded:bg-sidebar-accent"
          aria-label={m.nav.switchWorkspace}
        >
          <span className="flex size-8 shrink-0 items-center justify-center rounded-xl bg-primary text-sm font-semibold text-primary-foreground uppercase">
            {[...current.name][0] ?? "?"}
          </span>
          <span className="grid min-w-0 flex-1 leading-tight">
            <span className="truncate text-sm font-medium">{current.name}</span>
            <span className="truncate text-xs text-muted-foreground">
              {m.roles[current.role]} · {current.timezone}
            </span>
          </span>
          <ChevronsUpDownIcon className="size-4 shrink-0 text-muted-foreground" />
        </DropdownMenuTrigger>
        <DropdownMenuContent className="min-w-60">
          <DropdownMenuGroup>
            <DropdownMenuLabel>{m.nav.workspace}</DropdownMenuLabel>
            <DropdownMenuRadioGroup value={current.id} onValueChange={(v: string) => go(v)}>
              {(workspaces ?? [current]).map((w) => (
                <DropdownMenuRadioItem key={w.id} value={w.id}>
                  <span className="truncate">{w.name}</span>
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => setCreating(true)}>
            <PlusIcon />
            {m.nav.newWorkspace}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => void navigate({ to: "/" })}>
            <LayoutGridIcon />
            {m.nav.allWorkspaces}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <CreateWorkspaceDialog open={creating} onOpenChange={setCreating} />
    </>
  )
}
