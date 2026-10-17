import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  FilmIcon,
  ImageIcon,
  LibraryIcon,
  Loader2Icon,
  MusicIcon,
} from "lucide-react"
import { toast } from "sonner"

import type { RevisionMedia } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { revisionMediaQuery, useSaveRevisionMedia } from "@/queries/library"

type Ref = { storyId: string; deliverableId: string; number: number }

const kindIcon = { music: MusicIcon, video: FilmIcon, photo: ImageIcon }

/** The stock and music a video revision uses; off for other deliverables. */
export function useRevisionMedia(workspaceId: string, ref: Ref | undefined) {
  return useQuery({
    ...revisionMediaQuery(
      workspaceId,
      ref ?? { storyId: "", deliverableId: "", number: 0 }
    ),
    enabled: !!ref,
  })
}

/**
 * A revision's stock and music: what is in the library already (with a
 * link to it), and a button to save the rest.
 */
export function RevisionMediaList({
  workspaceId,
  revision,
  media,
  canEdit,
}: {
  workspaceId: string
  revision: Ref
  media: RevisionMedia[]
  canEdit: boolean
}) {
  const save = useSaveRevisionMedia(workspaceId, revision)
  const t = m.library.revision
  return (
    <ul className="grid gap-2 text-sm">
      {media.map((item) => {
        const Icon = kindIcon[item.kind]
        const saving = save.isPending && save.variables === item.key
        return (
          <li key={item.key} className="flex min-w-0 items-center gap-3">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <p className="truncate">{item.title}</p>
              <p className="truncate text-xs text-muted-foreground">
                {[t.sources[item.source], item.author]
                  .filter(Boolean)
                  .join(" · ")}
              </p>
            </div>
            {item.asset ? (
              <Link
                to="/w/$workspaceId/library"
                params={{ workspaceId }}
                search={{ asset: item.asset.id }}
                aria-label={t.open(item.title)}
                className="shrink-0"
              >
                <Badge variant="outline">
                  <LibraryIcon data-icon="inline-start" />
                  {item.asset.provisional ? t.provisional : t.inLibrary}
                </Badge>
              </Link>
            ) : null}
            {canEdit &&
            (!item.asset || item.asset.provisional) &&
            item.source !== "library" ? (
              <Button
                variant="outline"
                size="sm"
                className="shrink-0"
                disabled={save.isPending}
                onClick={() =>
                  save.mutate(item.key, {
                    onSuccess: (a) => toast.success(t.saved(a.title)),
                    onError: (err) => toast.error(errorMessage(err)),
                  })
                }
              >
                {saving ? (
                  <Loader2Icon
                    data-icon="inline-start"
                    className="animate-spin"
                  />
                ) : null}
                {saving ? t.saving : t.save}
              </Button>
            ) : null}
          </li>
        )
      })}
    </ul>
  )
}
