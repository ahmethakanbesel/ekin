import { useState } from "react"
import { Link } from "@tanstack/react-router"
import { SparklesIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import type { VisibilityPrompt } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { useAddPrompt, useDeletePrompt, useDraftPrompts, useUpdatePrompt } from "@/queries/visibility"

const t = m.visibility.prompts

/** The prompts buyers would ask: add, draft with the agent, switch off, remove. */
export function PromptsPanel({ prompts, setUp }: { prompts: VisibilityPrompt[]; setUp: boolean }) {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const add = useAddPrompt(workspace.id)
  const draft = useDraftPrompts(workspace.id)
  const [text, setText] = useState("")

  const submit = async () => {
    if (!text.trim()) return
    try {
      await add.mutateAsync(text)
      setText("")
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }
  const startDraft = async () => {
    try {
      await draft.mutateAsync(10)
      toast.success(t.drafting)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <div className="grid gap-5">
      {canEdit ? (
        <div className="flex flex-wrap items-center gap-2">
          <form
            className="flex min-w-0 flex-1 gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              void submit()
            }}
          >
            <Input
              aria-label={t.add}
              placeholder={t.placeholder}
              value={text}
              maxLength={500}
              onChange={(e) => setText(e.target.value)}
            />
            <Button type="submit" variant="outline" disabled={!text.trim() || add.isPending}>
              {t.add}
            </Button>
          </form>
          <Button variant="outline" disabled={!setUp || draft.isPending} onClick={() => void startDraft()} title={setUp ? undefined : t.setUpFirst}>
            <SparklesIcon data-icon="inline-start" />
            {t.draft}
          </Button>
        </div>
      ) : null}
      {prompts.length === 0 ? (
        <p className="rounded-xl border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">{t.empty}</p>
      ) : (
        <ul className="divide-y rounded-xl border">
          {prompts.map((p) => (
            <PromptRow key={p.id} prompt={p} />
          ))}
        </ul>
      )}
      <p className="text-xs text-muted-foreground">{t.count(prompts.length)}</p>
    </div>
  )
}

function PromptRow({ prompt: p }: { prompt: VisibilityPrompt }) {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const update = useUpdatePrompt(workspace.id)
  const remove = useDeletePrompt(workspace.id)
  const toggle = async (active: boolean) => {
    try {
      await update.mutateAsync({ id: p.id, active })
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }
  const del = async () => {
    try {
      await remove.mutateAsync(p.id)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }
  return (
    <li className="flex items-center gap-3 px-4 py-2.5">
      <Switch
        checked={p.active}
        disabled={!canEdit || update.isPending}
        onCheckedChange={(v) => void toggle(v)}
        aria-label={p.active ? t.turnOff : t.turnOn}
      />
      <div className="min-w-0 flex-1">
        <p className={p.active ? "text-sm" : "text-sm text-muted-foreground line-through decoration-1"}>{p.text}</p>
        <div className="mt-0.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {p.source === "agent" ? <Badge variant="outline">{t.byAgent}</Badge> : null}
          {p.coveredBy ? (
            <Link
              to="/w/$workspaceId/stories/$storyId"
              params={{ workspaceId: workspace.id, storyId: p.coveredBy }}
              className="text-primary hover:underline"
            >
              {t.covered}
            </Link>
          ) : null}
        </div>
      </div>
      {canEdit ? (
        <Button variant="ghost" size="icon-sm" aria-label={t.remove} disabled={remove.isPending} onClick={() => void del()}>
          <Trash2Icon />
        </Button>
      ) : null}
    </li>
  )
}
