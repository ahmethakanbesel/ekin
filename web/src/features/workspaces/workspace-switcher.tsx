import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { useNavigate, useRouterState } from "@tanstack/react-router"
import { ChevronsUpDownIcon, LayoutGridIcon, PlusIcon } from "lucide-react"

import type { Workspace } from "@/api/client"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
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
import { SidebarMenuButton, useSidebar } from "@/components/ui/sidebar"
import { m } from "@/i18n"
import { initials } from "@/lib/format"
import { workspacesQuery } from "@/queries/workspaces"
import { CreateWorkspaceDialog } from "./create-workspace-dialog"

const sectionRoutes = {
  stories: "/w/$workspaceId/stories",
  pipelines: "/w/$workspaceId/pipelines",
  knowledge: "/w/$workspaceId/knowledge",
  jobs: "/w/$workspaceId/jobs",
  settings: "/w/$workspaceId/settings",
} as const

/** The section of the current workspace URL, kept when switching. */
function currentSection(pathname: string): keyof typeof sectionRoutes {
  const part = pathname.split("/")[3]
  return part !== undefined && Object.hasOwn(sectionRoutes, part)
    ? (part as keyof typeof sectionRoutes)
    : "stories"
}

/**
 * The current workspace and a menu to switch. In the sidebar it is a large
 * menu button (an avatar alone when the sidebar is collapsed); `compact`
 * is the name and a chevron, for the mobile header.
 */
export function WorkspaceSwitcher({
  current,
  compact = false,
}: {
  current: Workspace
  compact?: boolean
}) {
  const { data: workspaces } = useQuery(workspacesQuery())
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const navigate = useNavigate()
  const { isMobile, state } = useSidebar()
  const [creating, setCreating] = useState(false)

  const go = (workspaceId: string) => {
    if (workspaceId === current.id) return
    void navigate({
      to: sectionRoutes[currentSection(pathname)],
      params: { workspaceId },
    })
  }

  const label = `${current.name}, ${m.roles[current.role]}. ${m.nav.switchWorkspace}`
  const trigger = compact ? (
    <DropdownMenuTrigger
      className="flex min-w-0 items-center gap-1.5 rounded-md px-2 py-1.5 text-sm font-medium outline-none hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring aria-expanded:bg-muted"
      aria-label={label}
    >
      <span className="truncate">{current.name}</span>
      <ChevronsUpDownIcon className="size-4 shrink-0 text-muted-foreground" />
    </DropdownMenuTrigger>
  ) : (
    <DropdownMenuTrigger
      render={
        <SidebarMenuButton
          size="lg"
          className="aria-expanded:bg-sidebar-accent aria-expanded:text-sidebar-accent-foreground"
        />
      }
      aria-label={label}
    >
      <Avatar className="rounded-lg after:rounded-lg">
        <AvatarFallback className="rounded-lg text-xs font-medium text-foreground">
          {initials(current.name)}
        </AvatarFallback>
      </Avatar>
      <span className="grid flex-1 text-left leading-tight">
        <span className="truncate font-medium">{current.name}</span>
        <span className="truncate text-xs text-muted-foreground">
          {m.roles[current.role]} · {current.timezone}
        </span>
      </span>
      <ChevronsUpDownIcon className="ml-auto text-muted-foreground" />
    </DropdownMenuTrigger>
  )

  return (
    <>
      <DropdownMenu>
        {trigger}
        <DropdownMenuContent
          className="min-w-60"
          side={!compact && !isMobile && state === "collapsed" ? "right" : "bottom"}
          align={compact ? "end" : "start"}
        >
          <DropdownMenuGroup>
            <DropdownMenuLabel>{m.nav.workspace}</DropdownMenuLabel>
            <DropdownMenuRadioGroup
              value={current.id}
              onValueChange={(v: string) => go(v)}
            >
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
