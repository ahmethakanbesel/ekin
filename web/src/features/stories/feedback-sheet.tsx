import { useEffect, type RefObject } from "react"
import { SendIcon, XIcon } from "lucide-react"

import type { Deliverable, Revision, Story } from "@/api/client"
import { Button } from "@/components/ui/button"
import { Kbd } from "@/components/ui/kbd"
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
import {
  FeedbackForm,
  type DraftStore,
  type FeedbackFocus,
  type FeedbackFooterState,
} from "./feedback-form"
import { showHistory } from "./review-panel"

type Props = {
  workspaceId: string
  story: Story
  deliverable: Deliverable
  revisions: Revision[]
  playhead: () => number | undefined
  focus: FeedbackFocus | null
  draft: DraftStore
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The rail button that opened it; focus goes back there on close. */
  returnFocus: RefObject<HTMLButtonElement | null>
  onSent: (story: Story) => void
}

/**
 * Feedback for the agent on the draft in review. From xl up it is docked
 * between the feed and the panel (`docked`); below, it is a sheet over the
 * page (from the bottom on phones). The unsent notes live in `draft`, so
 * they survive closing either.
 */
export function FeedbackSheet({ docked, ...props }: Props & { docked: boolean }) {
  return docked ? <DockedSheet {...props} /> : <ModalSheet {...props} />
}

type FormProps = Props & { onShowHistory: () => void }

function Footer({
  n,
  video,
  state,
}: {
  n: number
  video: boolean
  state: FeedbackFooterState
}) {
  return (
    <SheetFooter className="mt-auto border-t">
      <Button type="submit" disabled={state.sending} className="w-full">
        <SendIcon data-icon="inline-start" />
        {state.sending
          ? m.review.changes.sending
          : m.feedbackSheet.submit(state.count)}
      </Button>
      <p className="text-xs leading-relaxed text-muted-foreground">
        {m.feedbackSheet.hint(n)}
      </p>
      <p className="flex items-center gap-1.5 text-xs text-muted-foreground pointer-coarse:hidden">
        <Kbd>N</Kbd>
        {video ? m.feedbackSheet.pinKey : m.feedbackSheet.writeKey}
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
  draft,
  onSent,
  onShowHistory,
}: FormProps) {
  return (
    <FeedbackForm
      // Each draft gets its own note.
      key={`${story.id}/${d.id}/${d.revision}`}
      workspaceId={workspaceId}
      storyId={story.id}
      lang={story.language}
      deliverable={d}
      revisions={revisions}
      playhead={d.kind === "video" ? playhead : undefined}
      focus={focus}
      draft={draft}
      bodyClassName="scroll-area min-h-0 flex-1 overflow-y-auto px-4 pt-2 pb-6"
      renderFooter={(state) => (
        <Footer n={d.revision} video={d.kind === "video"} state={state} />
      )}
      onSent={onSent}
      onShowHistory={onShowHistory}
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
      <Form {...props} onShowHistory={showHistory} />
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
        <Form
          {...props}
          onShowHistory={() => {
            // The History is under the draft here; the sheet makes way.
            onOpenChange(false)
            window.setTimeout(showHistory, 250)
          }}
        />
      </SheetContent>
    </Sheet>
  )
}
