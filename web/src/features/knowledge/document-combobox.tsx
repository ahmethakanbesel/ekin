import { useMemo } from "react"

import type { Document } from "@/api/client"
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  ComboboxTrigger,
  ComboboxValue,
} from "@/components/ui/combobox"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { tokensLabel } from "./tokens"

/**
 * A searchable select over the workspace's ready documents, by id. A
 * value that is not among them (deleted, or not ready) still shows, so an
 * existing reference is never silently swapped.
 */
export function DocumentCombobox({
  id,
  value,
  documents,
  onChange,
  onBlur,
  disabled,
  invalid,
  className,
}: {
  id?: string
  value: string
  documents: Document[]
  onChange: (documentId: string) => void
  onBlur?: () => void
  disabled?: boolean
  invalid?: boolean
  className?: string
}) {
  const byId = useMemo(() => new Map(documents.map((d) => [d.id, d])), [documents])
  const items = useMemo(() => {
    const ready = documents.filter((d) => d.status === "ready").map((d) => d.id)
    return value && !ready.includes(value) ? [value, ...ready] : ready
  }, [documents, value])
  const label = (docId: string) => byId.get(docId)?.title ?? m.pipelineForm.unknownDocument

  return (
    <Combobox
      items={items}
      value={value || null}
      itemToStringLabel={label}
      onValueChange={(next: string | null) => {
        if (next) onChange(next)
      }}
      onOpenChange={(open) => {
        if (!open) onBlur?.()
      }}
      disabled={disabled}
    >
      <ComboboxTrigger
        id={id}
        disabled={disabled}
        aria-invalid={invalid || undefined}
        className={cn(
          "flex h-9 w-full min-w-0 items-center justify-between gap-1.5 rounded-3xl border border-transparent bg-input/50 px-3 py-2 text-sm whitespace-nowrap transition-[color,box-shadow,background-color] outline-none",
          "focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/30",
          "aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20",
          "disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50",
          className
        )}
      >
        <span className={cn("truncate", !value && "text-muted-foreground")}>
          <ComboboxValue placeholder={m.pipelineForm.documentPlaceholder} />
        </span>
      </ComboboxTrigger>
      <ComboboxContent className="min-w-80">
        <ComboboxInput placeholder={m.pipelineForm.documentSearch} showTrigger={false} />
        <ComboboxEmpty>
          {items.length ? m.pipelineForm.documentEmpty : m.pipelineForm.noDocuments}
        </ComboboxEmpty>
        <ComboboxList>
          {(docId: string) => {
            const doc = byId.get(docId)
            return (
              <ComboboxItem key={docId} value={docId}>
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate">{label(docId)}</span>
                  {doc ? (
                    <span className="text-xs font-normal text-muted-foreground tabular">
                      {m.pipelineForm.documentOption(
                        m.knowledge.pages(doc.pageCount),
                        tokensLabel(doc.tokens)
                      )}
                    </span>
                  ) : null}
                </div>
              </ComboboxItem>
            )
          }}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  )
}
