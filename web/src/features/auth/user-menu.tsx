import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { ChevronsUpDownIcon, LogOutIcon, ServerIcon, UserIcon } from "lucide-react"
import { toast } from "sonner"

import type { User } from "@/api/client"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { SidebarMenuButton, useSidebar } from "@/components/ui/sidebar"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { initials } from "@/lib/format"
import { meQuery, useLogout } from "@/queries/auth"

function UserAvatar({ user }: { user: User }) {
  return (
    <Avatar aria-hidden="true">
      <AvatarFallback className="text-xs font-medium text-foreground">
        {initials(user.name || user.email)}
      </AvatarFallback>
    </Avatar>
  )
}

/**
 * The signed-in user with Account and Sign out. `compact` shows only the
 * avatar (headers); otherwise a large sidebar menu button, which needs the
 * sidebar provider.
 */
export function UserMenu({ compact = false }: { compact?: boolean }) {
  return compact ? <UserMenuBody compact /> : <SidebarUserMenu />
}

function SidebarUserMenu() {
  const { isMobile, state } = useSidebar()
  return <UserMenuBody side={!isMobile && state === "collapsed" ? "right" : "top"} />
}

function UserMenuBody({
  compact = false,
  side = "bottom",
}: {
  compact?: boolean
  side?: "top" | "right" | "bottom"
}) {
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
      {compact ? (
        <DropdownMenuTrigger
          className="flex shrink-0 items-center rounded-full p-0.5 outline-none hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring aria-expanded:bg-muted"
          aria-label={m.auth.userMenu}
        >
          <UserAvatar user={me} />
        </DropdownMenuTrigger>
      ) : (
        <DropdownMenuTrigger
          render={
            <SidebarMenuButton
              size="lg"
              className="aria-expanded:bg-sidebar-accent aria-expanded:text-sidebar-accent-foreground"
            />
          }
          aria-label={`${me.name || me.email}. ${m.auth.userMenu}`}
        >
          <UserAvatar user={me} />
          <span className="grid flex-1 text-left leading-tight">
            <span className="truncate font-medium">{me.name}</span>
            <span className="truncate text-xs text-muted-foreground">{me.email}</span>
          </span>
          <ChevronsUpDownIcon className="ml-auto text-muted-foreground" />
        </DropdownMenuTrigger>
      )}
      <DropdownMenuContent
        side={side}
        align={compact || side === "top" ? "end" : "start"}
        className="min-w-60"
      >
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
