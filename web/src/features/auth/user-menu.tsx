import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { ChevronsUpDownIcon, LogOutIcon, ServerIcon, UserIcon } from "lucide-react"
import { toast } from "sonner"

import type { User } from "@/api/client"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { cn } from "@/lib/utils"
import { meQuery, useLogout } from "@/queries/auth"

function Avatar({ user, className }: { user: User; className?: string }) {
  return (
    <span
      className={cn(
        "flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-sm font-semibold text-muted-foreground uppercase",
        className,
      )}
      aria-hidden="true"
    >
      {[...(user.name || user.email)][0] ?? "?"}
    </span>
  )
}

/**
 * The signed-in user with Account and Sign out. `compact` shows only the
 * avatar (headers); otherwise a full-width row (the sidebar).
 */
export function UserMenu({ compact = false }: { compact?: boolean }) {
  const { data: me } = useQuery(meQuery())
  const logout = useLogout()
  const qc = useQueryClient()
  const navigate = useNavigate()

  if (!me) return null

  const signOut = async () => {
    try {
      await logout.mutateAsync()
    } catch (err) {
      toast.error(m.auth.signOutFailed, { description: errorMessage(err) })
      return
    }
    // Drop the cache before leaving so the sign-in page asks the server
    // afresh and nothing of this user survives for the next one.
    qc.removeQueries()
    await navigate({ to: "/login" })
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className={cn(
          "flex items-center gap-2.5 rounded-2xl text-left outline-none hover:bg-sidebar-accent focus-visible:ring-3 focus-visible:ring-ring/30 aria-expanded:bg-sidebar-accent",
          compact ? "rounded-full p-0.5" : "w-full px-2.5 py-2",
        )}
        aria-label={m.auth.userMenu}
      >
        <Avatar user={me} />
        {compact ? null : (
          <>
            <span className="grid min-w-0 flex-1 leading-tight">
              <span className="truncate text-sm font-medium">{me.name}</span>
              <span className="truncate text-xs text-muted-foreground">{me.email}</span>
            </span>
            <ChevronsUpDownIcon className="size-4 shrink-0 text-muted-foreground" />
          </>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align={compact ? "end" : "start"} className="min-w-60">
        <DropdownMenuGroup>
          <DropdownMenuLabel className="grid leading-tight">
            <span className="truncate text-sm font-medium text-foreground">{me.name}</span>
            <span className="truncate">{me.email}</span>
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => void navigate({ to: "/account" })}>
          <UserIcon />
          {m.auth.account}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => void navigate({ to: "/system" })}>
          <ServerIcon />
          {m.nav.system}
        </DropdownMenuItem>
        <DropdownMenuItem disabled={logout.isPending} onClick={() => void signOut()}>
          <LogOutIcon />
          {m.auth.signOut}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
