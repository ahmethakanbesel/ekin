import { useEffect, type RefObject } from "react"
import { SendIcon, XIcon } from "lucide-react"

import type { Deliverable, Revision, Story } from "@/api/client"
import { Button } from "@/components/ui/button"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { useMediaQuery } from "@/hooks/use-media-query"
import { m } from "@/i18n"
import { FeedbackForm, type FeedbackFocus } from "./feedback-form"

type Props = {
  workspaceId: string
  story: Story
  deliverable: Deliverable
  revisions: Revision[]
  playhead: () => number | undefined
  focus: FeedbackFocus | null
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The rail button that opened it; focus goes back there on close. */
  returnFocus: RefObject<HTMLButtonElement | null>
  onSent: (story: Story) => void
}

/**
 * Feedback for the agent on the draft in review. From xl up it is docked
 * between the feed and the panel (`docked`) and keeps its draft while
 * hidden; below, it is a sheet over the page (from the bottom on phones).
 */
export function FeedbackSheet({ docked, ...props }: Props & { docked: boolean }) {
  return docked ? <DockedSheet {...props} /> : <ModalSheet {...props} />
}

function Footer({ n, submitting }: { n: number; submitting: boolean }) {
  return (
    <SheetFooter className="mt-auto border-t">
      <Button type="submit" disabled={submitting} className="w-full">
        <SendIcon data-icon="inline-start" />
        {submitting ? m.review.changes.sending : m.feedbackSheet.submit}
      </Button>
      <p className="text-xs leading-relaxed text-muted-foreground">
        {m.feedbackSheet.hint(n)}
      </p>
    </SheetFooter>
  )
}

function Form({
  workspaceId,
  story,
  deliverable: d,
  revisions,
  playhead,
  focus,
  onSent,
}: Props) {
  return (
    <FeedbackForm
      // Each draft gets its own note.
      key={`${story.id}/${d.id}/${d.revision}`}
      workspaceId={workspaceId}
      storyId={story.id}
      deliverable={d}
      revisions={revisions}
      playhead={d.kind === "video" ? playhead : undefined}
      focus={focus}
      bodyClassName="scroll-area min-h-0 flex-1 overflow-y-auto px-4 pt-2 pb-6"
      renderFooter={(submitting) => <Footer n={d.revision} submitting={submitting} />}
      onSent={onSent}
    />
  )
}

function DockedSheet(props: Props) {
  const { deliverable: d, open, onOpenChange, returnFocus } = props

  // Esc closes it, unless a dialog or menu over it takes the key.
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return
      if (document.querySelector('[role="dialog"], [role="alertdialog"], [role="menu"]'))
        return
      onOpenChange(false)
      returnFocus.current?.focus()
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [open, onOpenChange, returnFocus])

  return (
    <aside
      id="feedback-sheet"
      hidden={!open}
      aria-labelledby="feedback-sheet-title"
      className="relative flex min-h-0 flex-col border-l bg-background"
    >
      <SheetHeader className="pr-12">
        <h2 id="feedback-sheet-title" className="font-semibold">
          {m.feedbackSheet.title}
        </h2>
        <p className="text-sm text-muted-foreground">
          {m.feedbackSheet.description(d.revision)}
        </p>
      </SheetHeader>
      <Button
        variant="ghost"
        size="icon-sm"
        className="absolute top-3 right-3"
        aria-label={m.feedbackSheet.close}
        onClick={() => {
          onOpenChange(false)
          returnFocus.current?.focus()
        }}
      >
        <XIcon />
      </Button>
      <Form {...props} />
    </aside>
  )
}

function ModalSheet(props: Props) {
  const { deliverable: d, open, onOpenChange } = props
  const phone = useMediaQuery("(max-width: 639px)")
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        id="feedback-sheet"
        side={phone ? "bottom" : "right"}
        className="gap-0 data-[side=bottom]:max-h-[88svh] data-[side=right]:w-full data-[side=right]:sm:max-w-md"
      >
        <SheetHeader className="pr-12">
          <SheetTitle className="font-semibold">
            {m.feedbackSheet.title}
          </SheetTitle>
          <SheetDescription>
            {m.feedbackSheet.description(d.revision)}
          </SheetDescription>
        </SheetHeader>
        <Form {...props} />
      </SheetContent>
    </Sheet>
  )
}
