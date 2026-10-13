import { useQuery } from "@tanstack/react-query"
import { Link, Outlet, useParams } from "@tanstack/react-router"
import { ListChecksIcon, SettingsIcon, UsersIcon, WorkflowIcon } from "lucide-react"

import { isApiError } from "@/api/client"
import { Brand } from "@/components/brand"
import { EmptyState, ErrorState } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { UserMenu } from "@/features/auth/user-menu"
import { m } from "@/i18n"
import { workspaceQuery } from "@/queries/workspaces"
import { WorkspaceContext } from "./context"
import { WorkspaceSwitcher } from "./workspace-switcher"

const navItems = [
  { to: "/w/$workspaceId/pipelines", label: m.nav.pipelines, icon: WorkflowIcon },
  { to: "/w/$workspaceId/jobs", label: m.nav.jobs, icon: ListChecksIcon },
  { to: "/w/$workspaceId/members", label: m.nav.members, icon: UsersIcon },
  { to: "/w/$workspaceId/settings", label: m.nav.settings, icon: SettingsIcon },
] as const

export function WorkspaceLayout() {
  const { workspaceId } = useParams({ from: "/w/$workspaceId" })
  const ws = useQuery(workspaceQuery(workspaceId))

  if (ws.isPending) {
    return (
      <div className="flex min-h-svh">
        <aside className="hidden w-64 shrink-0 border-r bg-sidebar p-3 md:block">
          <Skeleton className="h-12 w-full" />
        </aside>
        <main className="flex-1 p-8">
          <Skeleton className="h-8 w-48" />
        </main>
      </div>
    )
  }

  if (ws.isError) {
    // A workspace you are not a member of answers 404, like a missing one.
    const missing = isApiError(ws.error) && (ws.error.status === 404 || ws.error.status === 400)
    return (
      <main className="mx-auto max-w-xl p-8">
        {missing ? (
          <EmptyState
            title={m.workspace.notFound}
            action={
              <Button render={<Link to="/" />} nativeButton={false}>
                {m.errors.goHome}
              </Button>
            }
          />
        ) : (
          <ErrorState error={ws.error} onRetry={() => void ws.refetch()} />
        )}
      </main>
    )
  }

  const workspace = ws.data
  const nav = navItems.map((item) => (
    <Link
      key={item.to}
      to={item.to}
      params={{ workspaceId }}
      className="flex items-center gap-2.5 rounded-2xl px-3 py-2 text-sm font-medium text-sidebar-foreground/80 outline-none hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-3 focus-visible:ring-ring/30 data-[status=active]:bg-sidebar-accent data-[status=active]:text-sidebar-accent-foreground [&_svg]:size-4"
    >
      <item.icon />
      {item.label}
    </Link>
  ))

  return (
    <WorkspaceContext.Provider value={workspace}>
      <div className="flex min-h-svh">
        <aside className="sticky top-0 hidden h-svh w-64 shrink-0 flex-col gap-4 border-r bg-sidebar p-3 md:flex">
          <Brand />
          <WorkspaceSwitcher current={workspace} />
          <nav className="grid gap-0.5">{nav}</nav>
          <div className="mt-auto">
            <UserMenu />
          </div>
        </aside>
        <div className="flex min-w-0 flex-1 flex-col">
          <header className="flex flex-col gap-2 border-b bg-sidebar p-3 md:hidden">
            <div className="flex items-center gap-2">
              <Brand />
              <div className="ml-auto max-w-56 min-w-0 flex-1">
                <WorkspaceSwitcher current={workspace} />
              </div>
              <UserMenu compact />
            </div>
            <nav className="flex gap-1 overflow-x-auto">{nav}</nav>
          </header>
          <main className="mx-auto w-full max-w-7xl flex-1 p-4 md:p-8">
            <Outlet />
          </main>
        </div>
      </div>
    </WorkspaceContext.Provider>
  )
}
