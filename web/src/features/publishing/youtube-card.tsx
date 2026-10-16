import { useState } from "react"
import { Link } from "@tanstack/react-router"
import { ExternalLinkIcon, MonitorPlayIcon, UnplugIcon } from "lucide-react"
import { toast } from "sonner"

import type { Privacy, YouTubeIntegration } from "@/api/client"
import { Banner } from "@/components/page"
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatDateTime } from "@/lib/time"
import {
  useConnectYouTube,
  useDisconnectYouTube,
  useSetYouTubePrivacy,
} from "@/queries/integrations"

const PRIVACIES: Privacy[] = ["private", "unlisted", "public"]
const privacyItems = PRIVACIES.map((p) => ({ value: p, label: m.values.privacy[p] }))
const isPrivacy = (v: unknown): v is Privacy => PRIVACIES.includes(v as Privacy)

export function YouTubeCard({ youtube }: { youtube: YouTubeIntegration }) {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const connect = useConnectYouTube(workspace.id)
  const setPrivacy = useSetYouTubePrivacy(workspace.id)
  const [leaving, setLeaving] = useState(false)
  const [disconnecting, setDisconnecting] = useState(false)

  const startConnect = async () => {
    try {
      const url = await connect.mutateAsync()
      setLeaving(true)
      // A full page load: Google's consent screen, then back to settings.
      window.location.assign(url)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  const changePrivacy = async (privacy: Privacy) => {
    if (privacy === youtube.privacy) return
    try {
      await setPrivacy.mutateAsync(privacy)
      toast.success(m.youtube.privacySaved)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  const busy = connect.isPending || leaving
  const connectButton =
    canManage && youtube.configured ? (
      <Button
        variant={youtube.connected ? "outline" : "default"}
        disabled={busy}
        onClick={() => void startConnect()}
      >
        {busy ? m.youtube.connecting : youtube.connected ? m.youtube.reconnect : m.youtube.connect}
      </Button>
    ) : null

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <MonitorPlayIcon className="size-4 text-muted-foreground" />
          {m.youtube.title}
        </CardTitle>
        <CardDescription>{m.youtube.description}</CardDescription>
        {youtube.connected && canManage ? (
          <CardAction>
            <Button variant="ghost" size="sm" onClick={() => setDisconnecting(true)}>
              <UnplugIcon data-icon="inline-start" />
              {m.youtube.disconnect}
            </Button>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent className="grid gap-5">
        {youtube.configured ? null : (
          <Banner
            tone="warning"
            title={m.youtube.notConfiguredTitle}
            action={
              <Button
                size="sm"
                variant="outline"
                render={<Link to="/system" />}
                nativeButton={false}
              >
                {m.youtube.systemLink}
              </Button>
            }
          >
            <p>{m.youtube.notConfiguredBody}</p>
          </Banner>
        )}
        {youtube.connected ? (
          <>
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border px-4 py-3">
              <div className="min-w-0 space-y-0.5">
                <p className="text-xs text-muted-foreground">{m.youtube.channel}</p>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="truncate font-medium">
                    {youtube.channelTitle || youtube.channelId}
                  </span>
                  {youtube.channelId ? (
                    <a
                      href={`https://www.youtube.com/channel/${encodeURIComponent(youtube.channelId)}`}
                      target="_blank"
                      rel="noreferrer"
                      className="text-muted-foreground hover:text-foreground"
                      aria-label={m.youtube.openChannel}
                      title={m.youtube.openChannel}
                    >
                      <ExternalLinkIcon className="size-3.5" />
                    </a>
                  ) : null}
                </div>
                {youtube.connectedAt ? (
                  <p className="text-xs text-muted-foreground">
                    {m.youtube.connectedAt(formatDateTime(youtube.connectedAt, workspace.timezone))}
                  </p>
                ) : null}
              </div>
              {connectButton}
            </div>
            <Field>
              <FieldLabel htmlFor="yt-privacy">{m.youtube.privacy}</FieldLabel>
              <Select
                items={privacyItems}
                value={youtube.privacy ?? "private"}
                disabled={!canManage || setPrivacy.isPending}
                onValueChange={(v) => isPrivacy(v) && void changePrivacy(v)}
              >
                <SelectTrigger id="yt-privacy" className="max-w-48">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {privacyItems.map((it) => (
                    <SelectItem key={it.value} value={it.value}>
                      {it.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>{m.youtube.privacyHint}</FieldDescription>
            </Field>
          </>
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-dashed px-4 py-3">
            <div className="min-w-0 space-y-0.5">
              <p className="font-medium">{m.youtube.notConnected}</p>
              {youtube.configured ? (
                <p className="text-sm text-muted-foreground">{m.youtube.notConnectedBody}</p>
              ) : null}
            </div>
            {connectButton}
          </div>
        )}
      </CardContent>
      <DisconnectDialog
        open={disconnecting}
        channel={youtube.channelTitle || youtube.channelId || m.youtube.title}
        onOpenChange={setDisconnecting}
      />
    </Card>
  )
}

function DisconnectDialog({
  open,
  channel,
  onOpenChange,
}: {
  open: boolean
  channel: string
  onOpenChange: (open: boolean) => void
}) {
  const workspace = useWorkspace()
  const disconnect = useDisconnectYouTube(workspace.id)

  const confirm = async () => {
    try {
      await disconnect.mutateAsync()
      toast.success(m.youtube.disconnected)
      onOpenChange(false)
    } catch {
      // shown in the dialog
    }
  }

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) disconnect.reset()
        onOpenChange(next)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{m.youtube.disconnectTitle}</AlertDialogTitle>
          <AlertDialogDescription>{m.youtube.disconnectBody(channel)}</AlertDialogDescription>
        </AlertDialogHeader>
        {disconnect.error ? <Banner title={errorMessage(disconnect.error)} /> : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{m.common.cancel}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={disconnect.isPending}
            onClick={() => void confirm()}
          >
            {m.youtube.disconnect}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
