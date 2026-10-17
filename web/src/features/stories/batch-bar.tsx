import { useEffect, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { CheckIcon, XIcon } from "lucide-react"

import type { Story } from "@/api/client"
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
import { m } from "@/i18n"
import { rejectAll, reviewTargets, startBatchApproval } from "./batch-actions"

/** How many titles the reject dialog names before "and N more". */
const NAMED = 5

/**
 * Floats over the stories page while stories are selected: how many, and
 * approve or reject every draft of theirs waiting in review at once.
 * Approving waits a few seconds and can be undone; rejecting asks first.
 */
export function BatchBar({
  workspaceId,
  chosen,
  selectable,
  onSelectAll,
  onSelect,
}: {
  workspaceId: string
  /** The selected stories, each with a draft in review. */
  chosen: Story[]
  /** How many stories on the page could be selected. */
  selectable: number
  onSelectAll: () => void
  /** Replaces the selection (empty clears it). */
  onSelect: (storyIds: string[]) => void
}) {
  const qc = useQueryClient()
  const [rejecting, setRejecting] = useState(false)
  const [busy, setBusy] = useState(false)
  const targets = reviewTargets(chosen)

  // Esc clears the selection, unless a dialog or a field has it.
  useEffect(() => {
    if (rejecting) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return
      const el = e.target instanceof Element ? e.target : null
      if (
        el?.closest("input, textarea, select, [contenteditable], [role=dialog]")
      )
        return
      onSelect([])
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [rejecting, onSelect])

  const approve = () => {
    startBatchApproval({
      qc,
      workspaceId,
      targets,
      onUndo: (back) => onSelect([...new Set(back.map((t) => t.story.id))]),
    })
    onSelect([])
  }

  const reject = async () => {
    setBusy(true)
    try {
      await rejectAll({ qc, workspaceId, targets })
    } finally {
      setBusy(false)
      setRejecting(false)
      onSelect([])
    }
  }

  return (
    <>
      <div className="pointer-events-none fixed inset-x-0 bottom-4 z-40 flex justify-center px-4">
        <div
          role="region"
          aria-label={m.stories.batch.label}
          className="pointer-events-auto flex max-w-full flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border bg-background px-4 py-2.5 shadow-lg"
        >
          <p className="text-sm" aria-live="polite">
            <span className="tabular font-medium">
              {m.stories.batch.selected(chosen.length)}
            </span>
            <span className="text-muted-foreground">
              {" "}
              · {m.stories.batch.drafts(targets.length)}
            </span>
          </p>
          {chosen.length < selectable ? (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={onSelectAll}
            >
              {m.stories.batch.selectAll(selectable)}
            </Button>
          ) : null}
          <div className="flex items-center gap-2">
            <Button
              type="button"
              size="sm"
              onClick={approve}
              disabled={busy || targets.length === 0}
            >
              <CheckIcon data-icon="inline-start" />
              {m.stories.batch.approve}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="text-destructive hover:text-destructive"
              onClick={() => setRejecting(true)}
              disabled={busy || targets.length === 0}
            >
              {m.stories.batch.reject}
            </Button>
            <Button
              type="button"
              size="icon-sm"
              variant="ghost"
              aria-label={m.stories.batch.clear}
              title={`${m.stories.batch.clear} · ${m.stories.batch.hint}`}
              onClick={() => onSelect([])}
            >
              <XIcon />
            </Button>
          </div>
        </div>
      </div>

      <AlertDialog
        open={rejecting}
        onOpenChange={(open) => !busy && setRejecting(open)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {m.stories.batch.rejectTitle(targets.length)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {m.stories.batch.rejectBody(targets.length)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <ul className="grid gap-1 text-sm">
            {chosen.slice(0, NAMED).map((s) => (
              <li key={s.id} lang={s.language} className="truncate">
                {s.title}
              </li>
            ))}
            {chosen.length > NAMED ? (
              <li className="text-muted-foreground">
                {m.stories.batch.more(chosen.length - NAMED)}
              </li>
            ) : null}
          </ul>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={busy}>
              {m.common.cancel}
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={busy}
              onClick={() => void reject()}
            >
              {busy ? m.stories.batch.rejecting : m.review.reject}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
