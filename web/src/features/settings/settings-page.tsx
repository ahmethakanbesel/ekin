import type { ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, Outlet } from "@tanstack/react-router"
import { ArrowUpRightIcon } from "lucide-react"

import { PageHeader, ReadOnlyNote } from "@/components/page"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { meQuery } from "@/queries/auth"

/** The sections of workspace settings, in nav order; each is a child route. */
export const SETTINGS_SECTIONS = [
  "general",
  "production",
  "voice",
  "publishing",
  "library",
  "seo",
  "members",
] as const

export type SettingsSection = (typeof SETTINGS_SECTIONS)[number]

const ROUTES = {
  general: "/w/$workspaceId/settings/general",
  production: "/w/$workspaceId/settings/production",
  voice: "/w/$workspaceId/settings/voice",
  publishing: "/w/$workspaceId/settings/publishing",
  library: "/w/$workspaceId/settings/library",
  seo: "/w/$workspaceId/settings/seo",
  members: "/w/$workspaceId/settings/members",
} as const satisfies Record<SettingsSection, string>

const navLink =
  "flex h-8 shrink-0 items-center rounded-md px-2.5 text-sm whitespace-nowrap outline-none hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring data-[status=active]:bg-secondary data-[status=active]:font-medium data-[status=active]:inset-ring data-[status=active]:inset-ring-foreground/70"

/**
 * /w/:id/settings: the workspace layer. A section nav beside the section
 * itself; Server settings stay with the operator on /system.
 */
export function SettingsLayout() {
  const workspace = useWorkspace()
  const params = { workspaceId: workspace.id }
  return (
    <div className="max-w-6xl">
      <PageHeader
        title={m.settings.title}
        description={m.settings.description(workspace.name)}
      />
      <div className="grid gap-6 md:grid-cols-[12.5rem_minmax(0,1fr)] md:gap-8">
        <nav aria-label={m.settings.sectionsLabel} className="min-w-0">
          <ul className="-mx-4 flex gap-1 overflow-x-auto px-4 pb-1 md:mx-0 md:grid md:overflow-visible md:px-0 md:pb-0">
            {SETTINGS_SECTIONS.map((s) => (
              <li key={s}>
                <Link to={ROUTES[s]} params={params} className={navLink}>
                  {m.settings.sections[s].title}
                </Link>
              </li>
            ))}
          </ul>
          <ServerNote className="mt-5 hidden md:block" />
        </nav>
        <div className="min-w-0">
          <Outlet />
          <ServerNote className="mt-10 md:hidden" />
        </div>
      </div>
    </div>
  )
}

/** Where the layer above lives: Server, kept by the operator. */
function ServerNote({ className }: { className?: string }) {
  const me = useQuery(meQuery())
  return (
    <p className={cn("border-t pt-3 text-xs text-muted-foreground", className)}>
      {me.data?.isAdmin ? m.settings.serverNoteAdmin : m.settings.serverNote}{" "}
      <Link
        to="/system"
        className="inline-flex items-center gap-0.5 font-medium text-primary-ink underline-offset-4 hover:underline"
      >
        {m.settings.serverLink}
        <ArrowUpRightIcon className="size-3" aria-hidden />
      </Link>
    </p>
  )
}

/** The heading of one settings section, with its read-only note. */
export function SettingsSectionHeader({
  section,
  readOnly,
  actions,
}: {
  section: SettingsSection
  /** Shown when the viewer cannot change this section. */
  readOnly?: ReactNode
  actions?: ReactNode
}) {
  const t = m.settings.sections[section]
  return (
    <>
      <div className="mb-5 flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <h2 className="font-heading text-lg font-semibold tracking-tight">{t.title}</h2>
          <p className="text-sm text-muted-foreground">{t.description}</p>
        </div>
        {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
      </div>
      {readOnly ? <ReadOnlyNote>{readOnly}</ReadOnlyNote> : null}
    </>
  )
}
