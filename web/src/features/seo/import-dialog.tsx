import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { toast } from "sonner"

import type { Language } from "@/api/client"
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
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { seoProfileQuery, useImportOpportunities } from "@/queries/seo"
import { parseKeywords } from "./parse"
import { isLanguage, languageItems } from "./profile-card"

export function ImportDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        {/* Mounted only while open, so each opening starts blank. */}
        {open ? <ImportForm onDone={() => onOpenChange(false)} /> : null}
      </DialogContent>
    </Dialog>
  )
}

function ImportForm({ onDone }: { onDone: () => void }) {
  const workspace = useWorkspace()
  const profile = useQuery(seoProfileQuery(workspace.id))
  const importer = useImportOpportunities(workspace.id)
  const [text, setText] = useState("")
  const [chosen, setChosen] = useState<Language | null>(null)
  const [submitted, setSubmitted] = useState(false)
  const language: Language = chosen ?? profile.data?.language ?? "tr"
  const parsed = useMemo(() => parseKeywords(text), [text])
  const count = parsed.items.length
  const empty = submitted && count === 0

  const submit = async () => {
    setSubmitted(true)
    if (count === 0) return
    try {
      const added = await importer.mutateAsync(parsed.items.map((it) => ({ ...it, language })))
      toast.success(m.seo.imported(added, count - added))
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
        <DialogTitle>{m.seo.importTitle}</DialogTitle>
        <DialogDescription>{m.seo.importDescription}</DialogDescription>
      </DialogHeader>
      <Field data-invalid={empty || undefined}>
        <FieldLabel htmlFor="seo-import-text">{m.seo.importText}</FieldLabel>
        <Textarea
          id="seo-import-text"
          rows={10}
          spellCheck={false}
          placeholder={m.seo.importPlaceholder}
          className="max-h-80 min-h-48 font-mono text-[13px] md:text-[13px]"
          value={text}
          onChange={(e) => setText(e.target.value)}
          aria-invalid={empty || undefined}
          aria-describedby="seo-import-summary"
        />
        <div id="seo-import-summary" aria-live="polite" className="grid gap-0.5">
          {empty ? (
            <FieldError>{m.seo.importEmpty}</FieldError>
          ) : text.trim() ? (
            <FieldDescription>
              {m.seo.importPreview(count)}
              {parsed.columns
                ? ` · ${m.seo.importColumns(parsed.columns.map((c) => m.seo.columns[c].toLowerCase()).join(", "))}`
                : null}
            </FieldDescription>
          ) : null}
          {parsed.skipped > 0 ? (
            <FieldDescription className="text-amber-700 dark:text-amber-400">
              {m.seo.importSkipped(parsed.skipped)}
            </FieldDescription>
          ) : null}
        </div>
      </Field>
      <Field>
        <FieldLabel htmlFor="seo-import-language">{m.seo.importLanguage}</FieldLabel>
        <Select
          items={languageItems}
          value={language}
          onValueChange={(v) => isLanguage(v) && setChosen(v)}
        >
          <SelectTrigger id="seo-import-language" className="max-w-48">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {languageItems.map((it) => (
              <SelectItem key={it.value} value={it.value}>
                {it.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <FieldDescription>{m.seo.importLanguageHint}</FieldDescription>
      </Field>
      {importer.error ? <Banner title={errorMessage(importer.error)} /> : null}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          {m.common.cancel}
        </Button>
        <Button type="submit" disabled={importer.isPending}>
          {importer.isPending ? m.seo.importing : m.seo.importSubmit(count)}
        </Button>
      </DialogFooter>
    </form>
  )
}
