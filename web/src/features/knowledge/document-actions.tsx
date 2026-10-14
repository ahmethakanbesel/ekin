import { useState } from "react"
import { useForm } from "@tanstack/react-form"
import { useNavigate } from "@tanstack/react-router"
import {
  ExternalLinkIcon,
  MoreHorizontalIcon,
  PencilIcon,
  RotateCwIcon,
  Trash2Icon,
} from "lucide-react"
import { toast } from "sonner"

import type { Document } from "@/api/client"
import { Banner } from "@/components/page"
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Field, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { useRetained } from "@/lib/use-retained"
import { useDeleteDocument, useRenameDocument, useRetryDocument } from "@/queries/documents"

/** The server's title limit (domain MaxTitleLength). */
const MAX_TITLE = 200

/**
 * The "…" menu of a document: open, rename, retry a failed extraction,
 * delete. Only for editors; the caller decides whether to render it.
 */
export function DocumentMenu({
  workspaceId,
  doc,
  showOpen = false,
  onDeleted,
}: {
  workspaceId: string
  doc: Document
  /** Adds an "Open" item (the list; the viewer is already open). */
  showOpen?: boolean
  onDeleted?: () => void
}) {
  const navigate = useNavigate()
  const retry = useRetryDocument(workspaceId)
  const [renaming, setRenaming] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const doRetry = async () => {
    try {
      await retry.mutateAsync(doc.id)
      toast.success(m.knowledge.retried(doc.title))
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={<Button variant="ghost" size="icon-sm" aria-label={m.common.actions} />}
        >
          <MoreHorizontalIcon />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-48">
          {showOpen ? (
            <DropdownMenuItem
              onClick={() =>
                void navigate({
                  to: "/w/$workspaceId/knowledge/$documentId",
                  params: { workspaceId, documentId: doc.id },
                })
              }
            >
              <ExternalLinkIcon />
              {m.knowledge.open}
            </DropdownMenuItem>
          ) : null}
          <DropdownMenuItem onClick={() => setRenaming(true)}>
            <PencilIcon />
            {m.knowledge.rename}
          </DropdownMenuItem>
          {doc.status === "failed" ? (
            <DropdownMenuItem disabled={retry.isPending} onClick={() => void doRetry()}>
              <RotateCwIcon />
              {m.knowledge.retry}
            </DropdownMenuItem>
          ) : null}
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onClick={() => setDeleting(true)}>
            <Trash2Icon />
            {m.common.delete}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <RenameDocumentDialog
        workspaceId={workspaceId}
        doc={renaming ? doc : null}
        onOpenChange={setRenaming}
      />
      <DeleteDocumentDialog
        workspaceId={workspaceId}
        doc={deleting ? doc : null}
        onOpenChange={setDeleting}
        onDeleted={onDeleted}
      />
    </>
  )
}

function RenameDocumentDialog({
  workspaceId,
  doc,
  onOpenChange,
}: {
  workspaceId: string
  doc: Document | null
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={!!doc} onOpenChange={onOpenChange}>
      <DialogContent>
        {/* Mounted only while open, so each opening starts from the current title. */}
        {doc ? (
          <RenameForm workspaceId={workspaceId} doc={doc} onDone={() => onOpenChange(false)} />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

const titleError = (title: string) => {
  const t = title.trim()
  if (!t) return m.knowledge.titleRequired
  if ([...t].length > MAX_TITLE) return m.knowledge.titleTooLong
  return undefined
}

function RenameForm({
  workspaceId,
  doc,
  onDone,
}: {
  workspaceId: string
  doc: Document
  onDone: () => void
}) {
  const rename = useRenameDocument(workspaceId)
  const form = useForm({
    defaultValues: { title: doc.title },
    onSubmit: async ({ value }) => {
      const title = value.title.trim()
      if (title === doc.title) {
        onDone()
        return
      }
      try {
        await rename.mutateAsync({ documentId: doc.id, title })
        toast.success(m.knowledge.renamed)
        onDone()
      } catch {
        // shown in the banner
      }
    },
  })

  return (
    <form
      noValidate
      className="grid gap-6"
      onSubmit={(e) => {
        e.preventDefault()
        void form.handleSubmit()
      }}
    >
      <DialogHeader>
        <DialogTitle>{m.knowledge.renameTitle}</DialogTitle>
        <DialogDescription>{m.knowledge.renameDescription}</DialogDescription>
      </DialogHeader>
      <form.Field
        name="title"
        validators={{
          onChange: ({ value }) => titleError(value),
          onSubmit: ({ value }) => titleError(value),
        }}
      >
        {(field) => {
          const invalid = field.state.meta.isTouched && !field.state.meta.isValid
          return (
            <Field data-invalid={invalid || undefined}>
              <FieldLabel htmlFor="doc-title">{m.knowledge.titleLabel}</FieldLabel>
              <Input
                id="doc-title"
                autoComplete="off"
                maxLength={MAX_TITLE + 20}
                value={field.state.value}
                onChange={(e) => field.handleChange(e.target.value)}
                onBlur={field.handleBlur}
                aria-invalid={invalid || undefined}
              />
              {invalid ? <FieldError>{String(field.state.meta.errors[0])}</FieldError> : null}
            </Field>
          )
        }}
      </form.Field>
      {rename.error ? <Banner title={errorMessage(rename.error)} /> : null}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          {m.common.cancel}
        </Button>
        <form.Subscribe selector={(s) => s.isSubmitting}>
          {(submitting) => (
            <Button type="submit" disabled={submitting}>
              {submitting ? m.common.saving : m.common.save}
            </Button>
          )}
        </form.Subscribe>
      </DialogFooter>
    </form>
  )
}

/** Confirms a delete; the server's refusal (a pipeline uses it) stays in the dialog. */
function DeleteDocumentDialog({
  workspaceId,
  doc,
  onOpenChange,
  onDeleted,
}: {
  workspaceId: string
  doc: Document | null
  onOpenChange: (open: boolean) => void
  onDeleted?: () => void
}) {
  const target = useRetained(doc)
  const del = useDeleteDocument(workspaceId)

  const confirm = async () => {
    if (!target) return
    try {
      await del.mutateAsync(target.id)
    } catch {
      return // shown in the dialog
    }
    toast.success(m.knowledge.deleted(target.title))
    onOpenChange(false)
    onDeleted?.()
  }

  return (
    <AlertDialog
      open={!!doc}
      onOpenChange={(open) => {
        if (!open) del.reset()
        onOpenChange(open)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{m.knowledge.deleteTitle}</AlertDialogTitle>
          <AlertDialogDescription>{m.knowledge.deleteBody(target?.title ?? "")}</AlertDialogDescription>
        </AlertDialogHeader>
        {del.error ? <Banner title={errorMessage(del.error)} /> : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{m.common.cancel}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={del.isPending}
            onClick={() => void confirm()}
          >
            {m.common.delete}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
