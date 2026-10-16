import {
  useState,
  type ReactElement,
  type ReactNode,
  type RefObject,
} from "react"
import {
  CheckIcon,
  ExternalLinkIcon,
  LayersIcon,
  Loader2Icon,
  MessageSquarePlusIcon,
  PencilLineIcon,
  RotateCwIcon,
  SendIcon,
  Undo2Icon,
  XIcon,
} from "lucide-react"

import type { Deliverable } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { Kbd } from "@/components/ui/kbd"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { Button, buttonVariants } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { APPROVE_DELAY_MS, type PendingApproval } from "./pending-approvals"
import type { DeliverableReview } from "./use-deliverable-review"

/** Round rail buttons in their roles' colours; the label under each says what it does. */
const tones = {
  approve:
    "bg-success text-success-foreground inset-ring-success hover:bg-success/90",
  approving:
    "border-success text-success hover:bg-success/10 hover:text-success disabled:opacity-100 dark:border-success/70 dark:hover:bg-success/15",
  changes:
    "border-primary-ink text-primary-ink hover:bg-primary/10 hover:text-primary-ink dark:border-primary/70 dark:hover:bg-primary/15",
  neutral: "",
  reject:
    "border-destructive text-destructive hover:bg-destructive/10 hover:text-destructive dark:border-destructive/70 dark:hover:bg-destructive/20",
} as const

const round = "size-10 rounded-full"

/**
 * What can be done with the deliverable on screen, as a column beside the
 * draft (a row under it on phones). Shows only what applies to its status
 * and the viewer's role.
 */
export function ActionRail({
  deliverable: d,
  review,
  canEdit,
  onApprove,
  onRequestChanges,
  feedback,
}: {
  deliverable: Deliverable
  review: DeliverableReview
  canEdit: boolean
  /** Starts the (undoable) approval of the deliverable on screen. */
  onApprove: () => void
  /** Opens the feedback sheet on the note, pinned where the player paused. */
  onRequestChanges: () => void
  /** The feedback sheet's toggle. */
  feedback: {
    open: boolean
    onToggle: () => void
    ref: RefObject<HTMLButtonElement | null>
  }
}) {
  const busy = review.busy
  const { ref: feedbackRef, open: feedbackOpen, onToggle } = feedback
  const reviewing = canEdit && d.status === "review"
  const shown = review.current?.number ?? d.revision
  const main: ReactNode[] = []
  let reject: ReactNode = null

  const pending = review.pendingApproval
  // N does what Request changes does: pause, pin the playhead, write.
  const pinTip = (
    <>
      {d.kind === "video" ? m.feed.pinNote : m.feed.writeNote}
      <Kbd>N</Kbd>
    </>
  )

  if (reviewing) {
    main.push(
      pending ? (
        <RailItem
          key="approve"
          label={
            <>
              {m.review.approvingRail}
              {pending.state === "waiting" ? (
                <span className="block">{m.review.undo}</span>
              ) : null}
            </>
          }
          labelClass="text-success"
        >
          <Button
            variant="outline"
            size="icon-lg"
            className={cn(round, tones.approving)}
            aria-label={
              pending.state === "waiting"
                ? m.review.undoApprove(d.revision)
                : m.review.approving(d.revision)
            }
            disabled={pending.state !== "waiting"}
            onClick={review.undoApproval}
          >
            {pending.state === "waiting" ? (
              <Undo2Icon />
            ) : (
              <Loader2Icon className="animate-spin" />
            )}
          </Button>
          {pending.state === "waiting" ? (
            <CountdownRing pending={pending} />
          ) : null}
        </RailItem>
      ) : (
        <RailItem
          key="approve"
          label={m.review.approve}
          labelClass="text-success"
        >
          <Button
            size="icon-lg"
            className={cn(round, tones.approve)}
            aria-label={m.feed.approve(d.revision)}
            disabled={busy}
            onClick={onApprove}
          >
            <CheckIcon />
          </Button>
        </RailItem>
      ),
      <RailItem key="changes" label={m.review.requestChanges}>
        <RailTip
          tip={pinTip}
          button={
            <Button
              variant="outline"
              size="icon-lg"
              className={cn(round, tones.changes)}
              aria-label={m.feed.requestChanges(d.revision)}
              aria-keyshortcuts="N"
              disabled={busy}
              onClick={onRequestChanges}
            />
          }
        >
          <PencilLineIcon />
        </RailTip>
      </RailItem>,
      <RailItem key="feedback" label={m.feed.feedback}>
        <RailTip
          tip={
            <>
              {m.feed.feedbackToggle}
              <span aria-hidden="true">·</span>
              {pinTip}
            </>
          }
          button={
            <Button
              ref={feedbackRef}
              variant="outline"
              size="icon-lg"
              className={cn(round, tones.neutral)}
              aria-label={m.feed.feedbackToggle}
              aria-expanded={feedbackOpen}
              aria-controls="feedback-sheet"
              disabled={!!pending}
              onClick={onToggle}
            />
          }
        >
          <MessageSquarePlusIcon />
        </RailTip>
      </RailItem>
    )
  } else if (canEdit && d.status === "ready") {
    main.push(
      <RailItem key="publish" label={m.review.publish}>
        <Button
          size="icon-lg"
          className={round}
          aria-label={m.feed.publish}
          disabled={busy}
          onClick={() => review.setConfirmingPublish(true)}
        >
          <SendIcon />
        </Button>
      </RailItem>
    )
  } else if (canEdit && d.status === "failed") {
    main.push(
      <RailItem key="retry" label={m.review.retry}>
        <Button
          size="icon-lg"
          className={round}
          aria-label={m.feed.retry}
          disabled={busy}
          onClick={() => void review.retry()}
        >
          <RotateCwIcon />
        </Button>
      </RailItem>
    )
  }
  if (d.status === "published" && d.publication?.url) {
    main.push(
      <RailItem key="open" label={m.feed.open}>
        <a
          href={d.publication.url}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={m.feed.openPublication}
          className={cn(
            buttonVariants({ variant: "outline", size: "icon-lg" }),
            round
          )}
        >
          <ExternalLinkIcon />
        </a>
      </RailItem>
    )
  }
  if (review.list.length > 0) {
    main.push(
      <RailItem key="revisions" label={m.feed.revisions}>
        <RevisionsMenu review={review} shown={shown} />
      </RailItem>
    )
  }
  if (canEdit && (d.status === "review" || d.status === "failed")) {
    reject = (
      <RailItem label={m.review.reject} labelClass="text-destructive">
        <Button
          variant="outline"
          size="icon-lg"
          className={cn(round, tones.reject)}
          aria-label={m.feed.reject}
          disabled={busy}
          onClick={() => review.setRejecting(true)}
        >
          <XIcon />
        </Button>
      </RailItem>
    )
  }

  if (main.length === 0 && !reject) return <div className="rail" />
  return (
    <div
      className="rail flex flex-col items-center justify-end pb-1 max-sm:flex-row max-sm:items-start max-sm:justify-center max-sm:gap-1 max-sm:pb-0"
      role="group"
      aria-label={reviewing ? m.feed.rail(d.revision) : m.feed.railOther}
    >
      <div className="flex flex-col items-center gap-5 max-sm:flex-row max-sm:gap-1">
        {main}
      </div>
      {reject && main.length > 0 ? (
        <div
          role="none"
          className="my-5 h-px w-8 shrink-0 bg-border max-sm:mx-1 max-sm:my-0 max-sm:h-10 max-sm:w-px"
        />
      ) : null}
      {reject}
    </div>
  )
}

/** A rail button with a tooltip that names its key. */
function RailTip({
  tip,
  button,
  children,
}: {
  tip: ReactNode
  button: ReactElement
  /** The icon. */
  children: ReactNode
}) {
  return (
    <Tooltip>
      <TooltipTrigger render={button}>{children}</TooltipTrigger>
      <TooltipContent side="left">{tip}</TooltipContent>
    </Tooltip>
  )
}

function RailItem({
  label,
  labelClass,
  children,
}: {
  label: ReactNode
  labelClass?: string
  children: ReactNode
}) {
  return (
    <div className="flex w-16 flex-col items-center gap-1.5 sm:w-[88px]">
      <span className="relative inline-flex">{children}</span>
      <span
        className={cn(
          "text-center text-xs leading-tight font-medium text-balance",
          labelClass
        )}
        aria-hidden="true"
      >
        {label}
      </span>
    </div>
  )
}

/**
 * The undo window draining around the pending Approve button, resumed at
 * the right point when the rail mounts part-way through it.
 */
function CountdownRing({
  pending,
}: {
  pending: Extract<PendingApproval, { state: "waiting" }>
}) {
  const [left] = useState(() => Math.max(0, pending.deadline - Date.now()))
  return (
    <svg
      viewBox="0 0 48 48"
      className="pointer-events-none absolute -inset-1 size-12 -rotate-90 text-success motion-reduce:hidden"
      aria-hidden="true"
    >
      <circle
        cx="24"
        cy="24"
        r="22.5"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        pathLength={100}
        className="countdown-ring"
        style={{
          animationDuration: `${APPROVE_DELAY_MS}ms`,
          animationDelay: `${left - APPROVE_DELAY_MS}ms`,
        }}
      />
    </svg>
  )
}

/** Pick which revision to look at; the badge says which one is on screen. */
function RevisionsMenu({
  review,
  shown,
}: {
  review: DeliverableReview
  shown: number
}) {
  const latest = review.latest?.number
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="outline"
            size="icon-lg"
            className={cn(round, tones.neutral)}
            aria-label={m.feed.revisionsShowing(shown)}
          />
        }
      >
        <LayersIcon />
      </DropdownMenuTrigger>
      <Badge
        variant="secondary"
        className="pointer-events-none absolute -right-2 -bottom-1 h-5 rounded-full px-1.5 ring-2 ring-background"
        aria-hidden="true"
      >
        {m.stories.revisionShort(shown)}
      </Badge>
      <DropdownMenuContent side="left" align="end" className="min-w-44">
        <DropdownMenuGroup>
          <DropdownMenuLabel>{m.feed.revisions}</DropdownMenuLabel>
          <DropdownMenuRadioGroup
            value={String(shown)}
            onValueChange={(v) => review.pick(Number(v))}
          >
            {review.list.toReversed().map((r) => (
              <DropdownMenuRadioItem key={r.number} value={String(r.number)}>
                {m.feed.revisionItem(r.number)}
                {r.number === latest ? (
                  <span className="ml-auto text-xs text-muted-foreground">
                    {m.revisions.latest}
                  </span>
                ) : null}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
