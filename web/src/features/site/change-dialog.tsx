import { useState } from "react"
import { useNavigate } from "@tanstack/react-router"
import { toast } from "sonner"

import { Banner } from "@/components/page"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Textarea } from "@/components/ui/textarea"
import { Row } from "@/features/pipelines/form-parts"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { claimableFindings, useRequestChange } from "@/queries/site"
import { ruleText } from "./rules"

const t = m.site.change
const MAX_INSTRUCTIONS = 5000

/**
 * Starts a change: fixing a rule's open findings (rule set), or what the
 * instructions describe. Opens the change's story once it is queued.
 */
export function ChangeDialog({
  open,
  onOpenChange,
  rule,
  initial = "",
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The rule whose open findings to fix; none for a change described in words. */
  rule?: string
  /** Instructions to start from (a competitor's new page, say). */
  initial?: string
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {open ? <ChangeForm rule={rule} initial={initial} onDone={() => onOpenChange(false)} /> : null}
      </DialogContent>
    </Dialog>
  )
}

function ChangeForm({ rule, initial, onDone }: { rule?: string; initial: string; onDone: () => void }) {
  const workspace = useWorkspace()
  const navigate = useNavigate()
  const request = useRequestChange(workspace.id)
  const [instructions, setInstructions] = useState(initial)
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const tooLong = instructions.length > MAX_INSTRUCTIONS
  const missing = !rule && !instructions.trim()

  const submit = async () => {
    if (tooLong || missing) return
    setBusy(true)
    setError(undefined)
    try {
      let findingIds: string[] | undefined
      if (rule) {
        const open = await claimableFindings(workspace.id, rule)
        if (open.length === 0) {
          setError(t.nothingToFix)
          return
        }
        findingIds = open.map((f) => f.id)
      }
      const story = await request.mutateAsync({ findingIds, instructions: instructions.trim() || undefined })
      toast.success(t.started)
      onDone()
      void navigate({ to: "/w/$workspaceId/stories/$storyId", params: { workspaceId: workspace.id, storyId: story.id } })
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      noValidate
      className="grid gap-5"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <DialogHeader>
        <DialogTitle>{rule ? ruleText(rule).title : t.title}</DialogTitle>
        <DialogDescription>{rule ? t.fixBody : t.body}</DialogDescription>
      </DialogHeader>
      <Row
        label={rule ? t.notes : t.instructions}
        htmlFor="change-instructions"
        hint={rule ? t.notesHint : t.instructionsHint}
        error={tooLong ? t.tooLong : undefined}
      >
        <Textarea
          id="change-instructions"
          rows={5}
          value={instructions}
          placeholder={rule ? undefined : t.placeholder}
          aria-invalid={tooLong || undefined}
          onChange={(e) => setInstructions(e.target.value)}
        />
      </Row>
      {error ? <Banner title={error} /> : null}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          {m.common.cancel}
        </Button>
        <Button type="submit" disabled={busy || tooLong || missing}>
          {busy ? t.starting : t.start}
        </Button>
      </DialogFooter>
    </form>
  )
}
