import type { ReactElement, ReactNode } from "react"
import {
  CheckIcon,
  ExternalLinkIcon,
  LayersIcon,
  PencilLineIcon,
  RotateCwIcon,
  SendIcon,
  XIcon,
} from "lucide-react"

import type { Deliverable, Story } from "@/api/client"
import { Badge } from "@/components/ui/badge"
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
import type { DeliverableReview } from "./use-deliverable-review"

/** Round rail buttons in their roles' colours; the label under each says what it does. */
const tones = {
  approve:
    "bg-success text-success-foreground inset-ring-success hover:bg-success/90",
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
  onDecided,
  extra,
}: {
  deliverable: Deliverable
  review: DeliverableReview
  canEdit: boolean
  /** After an approval went through, with the updated story. */
  onDecided: (story: Story) => void
  /** More rail actions, after the decisions (the feedback sheet). */
  extra?: ReactNode
}) {
  const busy = review.busy
  const reviewing = canEdit && d.status === "review"
  const shown = review.current?.number ?? d.revision
  const main: ReactNode[] = []
  let reject: ReactNode = null

  if (reviewing) {
    main.push(
      <RailItem key="approve" label={m.review.approve} labelClass="text-success">
        <Button
          size="icon-lg"
          className={cn(round, tones.approve)}
          aria-label={m.feed.approve(d.revision)}
          disabled={busy}
          onClick={() =>
            void review.approve().then((s) => s && onDecided(s))
          }
        >
          <CheckIcon />
        </Button>
      </RailItem>,
      <RailItem key="changes" label={m.review.requestChanges}>
        <Button
          variant="outline"
          size="icon-lg"
          className={cn(round, tones.changes)}
          aria-label={m.feed.requestChanges(d.revision)}
          disabled={busy}
          onClick={review.openChanges}
        >
          <PencilLineIcon />
        </Button>
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
          onClick={() => void review.publish()}
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
  if (extra) main.push(extra)
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

function RailItem({
  label,
  labelClass,
  children,
}: {
  label: string
  labelClass?: string
  children: ReactElement
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
