import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { toast } from "sonner"

import type { LibrarySettings } from "@/api/client"
import { ErrorState } from "@/components/page"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { librarySettingsQuery, useSaveLibrarySettings } from "@/queries/library"

const KINDS = ["music", "video", "photo"] as const

/**
 * Which kinds the workspace saves to its library as drafts finish (all
 * off by default). Owners change it; others see it.
 */
export function LibrarySettingsCard({ className }: { className?: string }) {
  const workspace = useWorkspace()
  const settings = useQuery(librarySettingsQuery(workspace.id))
  const t = m.library.settings
  return (
    <Card id="library" className={className}>
      <CardHeader>
        <CardTitle>{t.title}</CardTitle>
        <CardDescription>{t.description}</CardDescription>
      </CardHeader>
      <CardContent>
        {settings.isPending ? (
          <div className="grid gap-3">
            {KINDS.map((k) => (
              <Skeleton key={k} className="h-10 w-full" />
            ))}
          </div>
        ) : settings.isError ? (
          <ErrorState
            error={settings.error}
            onRetry={() => void settings.refetch()}
          />
        ) : (
          // Re-mount when the stored settings change underneath the form.
          <SettingsForm
            key={JSON.stringify(settings.data)}
            saved={settings.data}
          />
        )}
      </CardContent>
    </Card>
  )
}

function SettingsForm({ saved }: { saved: LibrarySettings }) {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const save = useSaveLibrarySettings(workspace.id)
  const [value, setValue] = useState(saved)
  const dirty = KINDS.some((k) => value[k] !== saved[k])
  const t = m.library.settings
  const hints = { music: t.musicHint, video: t.videoHint, photo: t.photoHint }

  return (
    <form
      className="grid gap-5"
      onSubmit={(e) => {
        e.preventDefault()
        save.mutate(value, {
          onSuccess: () => toast.success(t.saved),
          onError: (err) => toast.error(errorMessage(err)),
        })
      }}
    >
      <div className="grid gap-4">
        {KINDS.map((k) => (
          <div key={k} className="flex items-start justify-between gap-4">
            <div className="space-y-0.5">
              <label
                htmlFor={`library-save-${k}`}
                className="text-sm font-medium"
              >
                {t[k]}
              </label>
              <p className="text-xs text-muted-foreground">{hints[k]}</p>
            </div>
            <Switch
              id={`library-save-${k}`}
              checked={value[k]}
              disabled={!canManage}
              onCheckedChange={(on) => setValue((v) => ({ ...v, [k]: on }))}
            />
          </div>
        ))}
      </div>
      {canManage ? (
        <div className="flex justify-end">
          <Button
            type="submit"
            variant={dirty ? "default" : "outline"}
            disabled={save.isPending || !dirty}
          >
            {save.isPending ? m.common.saving : m.common.save}
          </Button>
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">{t.readOnly}</p>
      )}
    </form>
  )
}
