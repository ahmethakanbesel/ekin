import { useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { LogOutIcon, MailIcon, UserMinusIcon, UserPlusIcon, XIcon } from "lucide-react"
import { toast } from "sonner"

import type { Invitation, Member, Role } from "@/api/client"
import { Banner, ErrorState, PageHeader, ReadOnlyNote, TableSkeleton } from "@/components/page"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatDateTime, formatRelative, useNow } from "@/lib/time"
import { useRetained } from "@/lib/use-retained"
import { meQuery } from "@/queries/auth"
import {
  forgetWorkspace,
  invitationsQuery,
  membersQuery,
  useChangeMemberRole,
  useRemoveMember,
  useRevokeInvitation,
} from "@/queries/members"
import { InviteDialog } from "./invite-dialog"
import { isRole, roleItems } from "./roles"

export function MembersPage() {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const selfId = useQuery(meQuery()).data?.id ?? ""
  const members = useQuery(membersQuery(workspace.id))
  const [inviting, setInviting] = useState(false)
  const [removing, setRemoving] = useState<Member | null>(null)
  const [leaving, setLeaving] = useState(false)
  const self = members.data?.find((x) => x.userId === selfId)

  let body
  if (members.isPending) body = <TableSkeleton rows={3} />
  else if (members.isError)
    body = <ErrorState error={members.error} onRetry={() => void members.refetch()} />
  else
    body = (
      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">{m.members.columns.name}</TableHead>
              <TableHead>{m.members.columns.email}</TableHead>
              <TableHead>{m.members.columns.role}</TableHead>
              <TableHead>{m.members.columns.joined}</TableHead>
              <TableHead className="w-12 pr-4">
                <span className="sr-only">{m.common.actions}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {members.data.map((member) => (
              <MemberRow
                key={member.userId}
                member={member}
                selfId={selfId}
                canManage={canManage}
                onRemove={() => setRemoving(member)}
              />
            ))}
          </TableBody>
        </Table>
      </div>
    )

  return (
    <div className="max-w-5xl">
      <PageHeader
        title={m.members.title}
        description={m.members.description}
        actions={
          <>
            {self ? (
              <Button variant="outline" onClick={() => setLeaving(true)}>
                <LogOutIcon data-icon="inline-start" />
                {m.members.leave}
              </Button>
            ) : null}
            {canManage ? (
              <Button onClick={() => setInviting(true)}>
                <UserPlusIcon data-icon="inline-start" />
                {m.members.invite}
              </Button>
            ) : null}
          </>
        }
      />
      {canManage ? null : <ReadOnlyNote>{m.members.manageHint}</ReadOnlyNote>}
      {body}
      {canManage ? <Invitations /> : null}
      <InviteDialog open={inviting} onOpenChange={setInviting} />
      <RemoveMemberDialog
        member={removing}
        selfId={selfId}
        onOpenChange={(open) => !open && setRemoving(null)}
      />
      <RemoveMemberDialog
        member={leaving ? (self ?? null) : null}
        selfId={selfId}
        onOpenChange={setLeaving}
      />
    </div>
  )
}

function MemberRow({
  member,
  selfId,
  canManage,
  onRemove,
}: {
  member: Member
  selfId: string
  canManage: boolean
  onRemove: () => void
}) {
  const workspace = useWorkspace()
  const change = useChangeMemberRole(workspace.id, selfId)
  const isSelf = member.userId === selfId

  const setRole = async (role: Role) => {
    if (role === member.role) return
    try {
      await change.mutateAsync({ userId: member.userId, role })
      toast.success(m.members.roleChanged(member.name, m.roles[role]))
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <TableRow>
      <TableCell className="pl-4">
        <div className="flex items-center gap-2">
          <span className="font-medium">{member.name}</span>
          {isSelf ? <Badge variant="outline">{m.members.you}</Badge> : null}
        </div>
      </TableCell>
      <TableCell className="text-sm text-muted-foreground">{member.email}</TableCell>
      <TableCell>
        {canManage ? (
          <Select
            items={roleItems}
            value={member.role}
            disabled={change.isPending}
            onValueChange={(v) => isRole(v) && void setRole(v)}
          >
            <SelectTrigger size="sm" className="w-32" aria-label={m.members.role}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {roleItems.map((it) => (
                <SelectItem key={it.value} value={it.value}>
                  {it.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <Badge variant={member.role === "owner" ? "secondary" : "outline"}>
            {m.roles[member.role]}
          </Badge>
        )}
      </TableCell>
      <TableCell className="tabular text-sm whitespace-nowrap">
        {formatDateTime(member.joinedAt, workspace.timezone)}
      </TableCell>
      <TableCell className="pr-4">
        {canManage && !isSelf ? (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={m.members.remove}
            title={m.members.remove}
            onClick={onRemove}
          >
            <UserMinusIcon />
          </Button>
        ) : null}
      </TableCell>
    </TableRow>
  )
}

/** Removes a member, or — for yourself — leaves the workspace. */
function RemoveMemberDialog({
  member,
  selfId,
  onOpenChange,
}: {
  member: Member | null
  selfId: string
  onOpenChange: (open: boolean) => void
}) {
  const workspace = useWorkspace()
  const target = useRetained(member)
  const remove = useRemoveMember(workspace.id, selfId)
  const navigate = useNavigate()
  const qc = useQueryClient()
  const leaving = target?.userId === selfId

  const confirm = async () => {
    if (!target) return
    try {
      await remove.mutateAsync(target.userId)
    } catch {
      return // shown in the dialog
    }
    if (leaving) {
      toast.success(m.members.left(workspace.name))
      await navigate({ to: "/" })
      forgetWorkspace(qc, workspace.id)
      return
    }
    toast.success(m.members.removed(target.name))
    onOpenChange(false)
  }

  return (
    <AlertDialog
      open={!!member}
      onOpenChange={(open) => {
        if (!open) remove.reset()
        onOpenChange(open)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {leaving ? m.members.leaveTitle : m.members.removeTitle}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {leaving
              ? m.members.leaveBody(workspace.name)
              : m.members.removeBody(target?.name ?? "", workspace.name)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {remove.error ? <Banner title={errorMessage(remove.error)} /> : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{m.common.cancel}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={remove.isPending}
            onClick={() => void confirm()}
          >
            {leaving ? m.members.leave : m.members.remove}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function Invitations() {
  const workspace = useWorkspace()
  const list = useQuery(invitationsQuery(workspace.id))
  const now = useNow()
  const [revoking, setRevoking] = useState<Invitation | null>(null)

  let body
  if (list.isPending) body = <TableSkeleton rows={2} />
  else if (list.isError)
    body = <ErrorState error={list.error} onRetry={() => void list.refetch()} />
  else if (list.data.length === 0)
    body = (
      <p className="flex items-center gap-2 rounded-xl border border-dashed px-4 py-6 text-sm text-muted-foreground">
        <MailIcon className="size-4 shrink-0" />
        {m.members.invitationsEmpty}
      </p>
    )
  else
    body = (
      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">{m.members.columns.email}</TableHead>
              <TableHead>{m.members.columns.role}</TableHead>
              <TableHead>{m.members.columns.invited}</TableHead>
              <TableHead>{m.members.columns.expires}</TableHead>
              <TableHead className="w-12 pr-4">
                <span className="sr-only">{m.common.actions}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.data.map((inv) => (
              <TableRow key={inv.id}>
                <TableCell className="pl-4 font-medium">{inv.email}</TableCell>
                <TableCell>
                  <Badge variant="secondary">{m.roles[inv.role]}</Badge>
                </TableCell>
                <TableCell className="tabular text-sm whitespace-nowrap">
                  {formatDateTime(inv.createdAt, workspace.timezone)}
                </TableCell>
                <TableCell className="whitespace-nowrap">
                  <div className="flex flex-col gap-0.5">
                    <span className="tabular text-sm">
                      {formatDateTime(inv.expiresAt, workspace.timezone)}
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {formatRelative(inv.expiresAt, now)}
                    </span>
                  </div>
                </TableCell>
                <TableCell className="pr-4">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={m.members.revoke}
                    title={m.members.revoke}
                    onClick={() => setRevoking(inv)}
                  >
                    <XIcon />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    )

  return (
    <section className="mt-10">
      <h2 className="mb-3 font-heading text-base font-medium">{m.members.invitationsTitle}</h2>
      {body}
      <RevokeDialog invitation={revoking} onOpenChange={(open) => !open && setRevoking(null)} />
    </section>
  )
}

function RevokeDialog({
  invitation,
  onOpenChange,
}: {
  invitation: Invitation | null
  onOpenChange: (open: boolean) => void
}) {
  const workspace = useWorkspace()
  const target = useRetained(invitation)
  const revoke = useRevokeInvitation(workspace.id)

  const confirm = async () => {
    if (!target) return
    try {
      await revoke.mutateAsync(target.id)
      toast.success(m.members.revoked)
      onOpenChange(false)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <AlertDialog open={!!invitation} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{m.members.revokeTitle}</AlertDialogTitle>
          <AlertDialogDescription>
            {m.members.revokeBody(target?.email ?? "")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{m.members.keep}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={revoke.isPending}
            onClick={() => void confirm()}
          >
            {m.members.revoke}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
