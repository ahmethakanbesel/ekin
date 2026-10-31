import { useState } from "react"
import { Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { RefreshCwIcon, SearchIcon, UnplugIcon } from "lucide-react"
import { toast } from "sonner"

import type { SearchConsole } from "@/api/client"
import { Banner, ErrorState } from "@/components/page"
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
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatDateTime } from "@/lib/time"
import {
  searchConsoleQuery,
  searchPropertiesQuery,
  useChooseSearchProperty,
  useConnectSearchConsole,
  useDisconnectSearchConsole,
  useSyncSearchConsole,
} from "@/queries/search"

const t = m.searchConsole

/** Settings › Publishing: the Search Console property Ekin reads. */
export function SearchConsoleCard() {
  const workspace = useWorkspace()
  const query = useQuery(searchConsoleQuery(workspace.id))
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <SearchIcon className="size-4 text-muted-foreground" />
          {t.title}
        </CardTitle>
        <CardDescription>{t.description}</CardDescription>
        {query.data?.connected ? <DisconnectAction /> : null}
      </CardHeader>
      <CardContent className="grid gap-5">
        {query.isPending ? (
          <Skeleton className="h-16 w-full rounded-xl" />
        ) : query.isError ? (
          <ErrorState error={query.error} onRetry={() => void query.refetch()} />
        ) : (
          <Body sc={query.data} />
        )}
      </CardContent>
    </Card>
  )
}

function Body({ sc }: { sc: SearchConsole }) {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const canEdit = useCan("edit")
  const connect = useConnectSearchConsole(workspace.id)
  const sync = useSyncSearchConsole(workspace.id)
  const [leaving, setLeaving] = useState(false)

  const startConnect = async () => {
    try {
      const url = await connect.mutateAsync()
      setLeaving(true)
      window.location.assign(url)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }
  const busy = connect.isPending || leaving
  const connectButton =
    canManage && sc.configured ? (
      <Button variant={sc.connected ? "outline" : "default"} disabled={busy} onClick={() => void startConnect()}>
        {busy ? t.connecting : sc.connected ? t.reconnect : t.connect}
      </Button>
    ) : null

  if (!sc.configured)
    return (
      <Banner
        tone="warning"
        title={t.notConfiguredTitle}
        action={
          <Button size="sm" variant="outline" render={<Link to="/system" />} nativeButton={false}>
            {m.youtube.systemLink}
          </Button>
        }
      >
        <p>{t.notConfiguredBody}</p>
      </Banner>
    )
  if (!sc.connected)
    return (
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-dashed px-4 py-3">
        <div className="min-w-0 space-y-0.5">
          <p className="font-medium">{t.notConnected}</p>
          <p className="text-sm text-muted-foreground">{t.notConnectedBody}</p>
        </div>
        {connectButton}
      </div>
    )

  const startSync = async () => {
    try {
      await sync.mutateAsync()
      toast.success(t.syncStarted)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <>
      {sc.lastError ? (
        <Banner title={t.syncFailed}>
          <p className="break-words">{sc.lastError}</p>
        </Banner>
      ) : null}
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border px-4 py-3">
        <div className="min-w-0 space-y-0.5">
          <p className="text-xs text-muted-foreground">{t.status}</p>
          <p className="font-medium">
            {sc.syncedThrough ? t.syncedThrough(sc.syncedThrough) : sc.property ? t.firstSync : t.chooseFirst}
          </p>
          {sc.lastSyncAt ? (
            <p className="text-xs text-muted-foreground">{t.lastSync(formatDateTime(sc.lastSyncAt, workspace.timezone))}</p>
          ) : null}
        </div>
        <div className="flex flex-wrap gap-2">
          {canEdit && sc.property ? (
            <Button variant="outline" disabled={sync.isPending} onClick={() => void startSync()}>
              <RefreshCwIcon data-icon="inline-start" />
              {t.syncNow}
            </Button>
          ) : null}
          {connectButton}
        </div>
      </div>
      <PropertyField sc={sc} />
    </>
  )
}

function PropertyField({ sc }: { sc: SearchConsole }) {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const props = useQuery({ ...searchPropertiesQuery(workspace.id), enabled: canManage })
  const choose = useChooseSearchProperty(workspace.id)

  const pick = async (property: string) => {
    if (property === sc.property) return
    try {
      await choose.mutateAsync(property)
      toast.success(t.propertySaved)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  if (!canManage)
    return (
      <p className="text-sm">
        <span className="text-muted-foreground">{t.property}: </span>
        {sc.property ?? t.none}
      </p>
    )
  const items = (props.data?.properties ?? []).map((p) => ({ value: p.url, label: p.url }))
  if (sc.property && !items.some((i) => i.value === sc.property)) items.unshift({ value: sc.property, label: sc.property })
  return (
    <Field>
      <FieldLabel htmlFor="sc-property">{t.property}</FieldLabel>
      <Select
        items={items}
        value={sc.property ?? null}
        disabled={choose.isPending || props.isPending}
        onValueChange={(v) => typeof v === "string" && void pick(v)}
      >
        <SelectTrigger id="sc-property" className="w-full max-w-md font-mono text-sm">
          <SelectValue placeholder={props.isPending ? m.common.loading : t.choose} />
        </SelectTrigger>
        <SelectContent>
          {items.map((it) => (
            <SelectItem key={it.value} value={it.value} className="font-mono text-sm">
              {it.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <FieldDescription>{props.isError ? errorMessage(props.error) : t.propertyHint}</FieldDescription>
    </Field>
  )
}

function DisconnectAction() {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const disconnect = useDisconnectSearchConsole(workspace.id)
  const [open, setOpen] = useState(false)
  if (!canManage) return null
  const confirm = async () => {
    try {
      await disconnect.mutateAsync()
      toast.success(t.disconnected)
      setOpen(false)
    } catch {
      // shown in the dialog
    }
  }
  return (
    <CardAction>
      <Button variant="ghost" size="sm" onClick={() => setOpen(true)}>
        <UnplugIcon data-icon="inline-start" />
        {t.disconnect}
      </Button>
      <AlertDialog
        open={open}
        onOpenChange={(next) => {
          if (!next) disconnect.reset()
          setOpen(next)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t.disconnectTitle}</AlertDialogTitle>
            <AlertDialogDescription>{t.disconnectBody}</AlertDialogDescription>
          </AlertDialogHeader>
          {disconnect.error ? <Banner title={errorMessage(disconnect.error)} /> : null}
          <AlertDialogFooter>
            <AlertDialogCancel>{m.common.cancel}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={disconnect.isPending} onClick={() => void confirm()}>
              {t.disconnect}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </CardAction>
  )
}
