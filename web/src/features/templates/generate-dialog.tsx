import { useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { toast } from "sonner"

import type { DeliverableKind, Template } from "@/api/client"
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
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Row } from "@/features/pipelines/form-parts"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { keys } from "@/queries/keys"
import { useGenerateTemplate } from "@/queries/templates"
import { watchDraft } from "./draft-watch"
import { KindsCheckboxes } from "./kinds-checkboxes"
import { TEMPLATE_LIMITS } from "./model"

export function GenerateTemplateDialog({
  open,
  onOpenChange,
  defaultKind,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  defaultKind: DeliverableKind
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {open ? <GenerateForm defaultKind={defaultKind} onDone={() => onOpenChange(false)} /> : null}
      </DialogContent>
    </Dialog>
  )
}

function GenerateForm({ defaultKind, onDone }: { defaultKind: DeliverableKind; onDone: () => void }) {
  const workspace = useWorkspace()
  const generate = useGenerateTemplate(workspace.id)
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [channel, setChannel] = useState("")
  const [notes, setNotes] = useState("")
  const [kinds, setKinds] = useState<DeliverableKind[]>([defaultKind])
  const [submitted, setSubmitted] = useState(false)
  const t = m.templates.generate

  const trimmed = channel.trim()
  let channelError: string | undefined
  if (!trimmed) channelError = t.channelRequired
  else if ([...trimmed].length > TEMPLATE_LIMITS.channel) channelError = t.channelTooLong
  const notesError = [...notes.trim()].length > TEMPLATE_LIMITS.notes ? t.notesTooLong : undefined
  const kindsError = kinds.length === 0 ? t.kindsRequired : undefined
  const valid = !channelError && !notesError && !kindsError

  const submit = async () => {
    setSubmitted(true)
    if (!valid) return
    try {
      const job = await generate.mutateAsync({
        channel: trimmed,
        notes: notes.trim() || undefined,
        kinds,
      })
      watchDraft(
        workspace.id,
        job.id,
        trimmed,
        qc.getQueryData<Template[]>(keys.templates(workspace.id))
      )
      toast.success(t.queued, {
        description: t.queuedBody,
        action: {
          label: t.viewJobs,
          onClick: () =>
            void navigate({ to: "/w/$workspaceId/jobs", params: { workspaceId: workspace.id } }),
        },
      })
      onDone()
    } catch {
      // shown in the banner
    }
  }

  return (
    <form
      noValidate
      className="grid gap-6"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <DialogHeader>
        <DialogTitle>{t.title}</DialogTitle>
        <DialogDescription>{t.description}</DialogDescription>
      </DialogHeader>
      <Row
        label={t.channel}
        htmlFor="tpl-gen-channel"
        error={submitted ? channelError : undefined}
      >
        <Input
          id="tpl-gen-channel"
          autoComplete="off"
          spellCheck={false}
          placeholder={t.channelPlaceholder}
          value={channel}
          onChange={(e) => setChannel(e.target.value)}
          aria-invalid={(submitted && !!channelError) || undefined}
        />
      </Row>
      <Row label={t.notes} htmlFor="tpl-gen-notes" hint={t.notesHint} error={notesError}>
        <Textarea
          id="tpl-gen-notes"
          rows={3}
          className="min-h-20"
          placeholder={t.notesPlaceholder}
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          aria-invalid={!!notesError || undefined}
        />
      </Row>
      <Row label={t.kinds} error={submitted ? kindsError : undefined}>
        <KindsCheckboxes
          idPrefix="tpl-gen-kind"
          value={kinds}
          onChange={setKinds}
          invalid={submitted && !!kindsError}
        />
      </Row>
      {generate.error ? <Banner title={errorMessage(generate.error)} /> : null}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          {m.common.cancel}
        </Button>
        <Button type="submit" disabled={generate.isPending}>
          {generate.isPending ? t.submitting : t.submit}
        </Button>
      </DialogFooter>
    </form>
  )
}
