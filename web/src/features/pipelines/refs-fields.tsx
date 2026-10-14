import { useMemo } from "react"
import { useStore } from "@tanstack/react-form"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { PlusIcon, Trash2Icon } from "lucide-react"

import type { Document } from "@/api/client"
import { Button } from "@/components/ui/button"
import { Field, FieldError, FieldLabel } from "@/components/ui/field"
import { DocumentCombobox } from "@/features/knowledge/document-combobox"
import {
  tokensLabel,
  useTokenEstimates,
  type RangeRequest,
  type TokenEstimate,
} from "@/features/knowledge/tokens"
import { m } from "@/i18n"
import { withFieldGroup } from "@/lib/form"
import { documentsQuery } from "@/queries/documents"
import { NumberInput, Row, shownError } from "./form-parts"
import { LIMITS, emptyRef, toInt, type RefValues } from "./model"

const defaultRefs: { refs: RefValues[] } = { refs: [] }

/**
 * Knowledge-base references: rows of document + optional page range, with
 * token estimates. Errors come from the host form's validator
 * (validateRefs, keyed by the form's path to the list).
 */
export const RefsFields = withFieldGroup({
  defaultValues: defaultRefs,
  props: {
    workspaceId: "",
    label: "",
    hint: undefined as string | undefined,
    readOnly: false,
    submitted: false,
    idPrefix: "refs",
  },
  render: function RefsFieldsGroup({
    group,
    workspaceId,
    label,
    hint,
    readOnly,
    submitted,
    idPrefix,
  }) {
    const documents = useQuery(documentsQuery(workspaceId))
    const refs = useStore(group.store, (s) => s.values.refs)
    // The range error sits on fromPage but concerns both pages, so it is
    // shown once either page was touched.
    const fieldMeta = useStore(group.form.store, (s) => s.fieldMeta)
    const docsById = useMemo(
      () => new Map((documents.data ?? []).map((d) => [d.id.toLowerCase(), d])),
      [documents.data]
    )
    const estimates = useTokenEstimates(
      workspaceId,
      refs.map((r) => rangeRequest(r, docsById))
    )
    const total = sumEstimates(estimates)
    const shown = (meta: { isTouched: boolean; errors: unknown[] }) => shownError(meta, submitted)

    const rangeError = (i: number): string | undefined => {
      const from = fieldMeta[group.getFormFieldName(`refs[${i}].fromPage`)]
      const to = fieldMeta[group.getFormFieldName(`refs[${i}].toPage`)]
      const touched = from?.isTouched || to?.isTouched || submitted
      return touched ? (from?.errors[0] as string | undefined) : undefined
    }

    return (
      <group.Field name="refs" mode="array">
        {(refsField) => (
          <Row label={label} hint={hint} error={shown(refsField.state.meta)}>
            <div className="grid gap-3">
              {refsField.state.value.map((_, i) => {
                const pages = docsById.get(refs[i]?.documentId.toLowerCase() ?? "")?.pageCount
                const range = rangeError(i)
                return (
                  <div
                    key={i}
                    className="grid items-start gap-2 rounded-3xl border p-3 sm:grid-cols-[minmax(0,1fr)_7rem_7rem_auto]"
                  >
                    <group.Field name={`refs[${i}].documentId`}>
                      {(field) => (
                        <Field
                          className="min-w-0"
                          data-invalid={!!shown(field.state.meta) || undefined}
                        >
                          <FieldLabel htmlFor={`${idPrefix}-${i}-doc`} className="sr-only">
                            {m.pipelineForm.document}
                          </FieldLabel>
                          <DocumentCombobox
                            id={`${idPrefix}-${i}-doc`}
                            disabled={readOnly}
                            documents={documents.data ?? []}
                            value={field.state.value}
                            onChange={field.handleChange}
                            onBlur={field.handleBlur}
                            invalid={!!shown(field.state.meta)}
                          />
                          {shown(field.state.meta) ? (
                            <FieldError>{shown(field.state.meta)}</FieldError>
                          ) : null}
                        </Field>
                      )}
                    </group.Field>
                    <group.Field name={`refs[${i}].fromPage`}>
                      {(field) => (
                        <>
                          <FieldLabel htmlFor={`${idPrefix}-${i}-from`} className="sr-only">
                            {m.pipelineForm.fromPage}
                          </FieldLabel>
                          <NumberInput
                            id={`${idPrefix}-${i}-from`}
                            placeholder={m.pipelineForm.fromPage}
                            value={field.state.value}
                            onChange={field.handleChange}
                            onBlur={field.handleBlur}
                            min={1}
                            max={pages}
                            invalid={!!range}
                          />
                        </>
                      )}
                    </group.Field>
                    <group.Field name={`refs[${i}].toPage`}>
                      {(field) => (
                        <>
                          <FieldLabel htmlFor={`${idPrefix}-${i}-to`} className="sr-only">
                            {m.pipelineForm.toPage}
                          </FieldLabel>
                          <NumberInput
                            id={`${idPrefix}-${i}-to`}
                            placeholder={m.pipelineForm.toPage}
                            value={field.state.value}
                            onChange={field.handleChange}
                            onBlur={field.handleBlur}
                            min={1}
                            max={pages}
                          />
                        </>
                      )}
                    </group.Field>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={m.pipelineForm.removeRef}
                      onClick={() => refsField.removeValue(i)}
                    >
                      <Trash2Icon />
                    </Button>
                    {range ? <FieldError className="sm:col-span-4">{range}</FieldError> : null}
                    <RefEstimate estimate={estimates[i]} />
                  </div>
                )
              })}
              {documents.data && !documents.data.some((d) => d.status === "ready") && !readOnly ? (
                <p className="text-sm text-muted-foreground">
                  {m.pipelineForm.noDocuments}{" "}
                  <Link
                    to="/w/$workspaceId/knowledge"
                    params={{ workspaceId }}
                    className="font-medium text-foreground underline-offset-4 hover:underline"
                  >
                    {m.pipelineForm.uploadDocuments}
                  </Link>
                </p>
              ) : null}
              <div className="flex flex-wrap items-center justify-between gap-3">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={refsField.state.value.length >= LIMITS.refs}
                  onClick={() => refsField.pushValue(emptyRef())}
                >
                  <PlusIcon data-icon="inline-start" />
                  {m.pipelineForm.addRef}
                </Button>
                {total ? (
                  <p
                    className="text-sm font-medium tabular"
                    title={m.pipelineForm.tokensHint}
                    aria-live="polite"
                  >
                    {m.pipelineForm.refsTotal(tokensLabel(total.tokens, total.approximate))}
                  </p>
                ) : null}
              </div>
            </div>
          </Row>
        )}
      </group.Field>
    )
  },
})

/** The token range a reference row describes; blank pages mean the whole document. */
function rangeRequest(r: RefValues, docs: ReadonlyMap<string, Document>): RangeRequest {
  const doc = docs.get(r.documentId.trim().toLowerCase())
  if (r.fromPage.trim() === "" && r.toPage.trim() === "") return { doc }
  const from = toInt(r.fromPage)
  const to = toInt(r.toPage)
  if (from === undefined || to === undefined) return { doc: undefined }
  return { doc, from, to }
}

function sumEstimates(
  estimates: Array<TokenEstimate | undefined>
): { tokens: number; approximate: boolean } | undefined {
  const known = estimates.filter((e) => e !== undefined)
  if (known.length === 0) return undefined
  return {
    tokens: known.reduce((sum, e) => sum + e.tokens, 0),
    approximate: known.some((e) => e.approximate),
  }
}

/** "Pages 12–40 · 29 pages · ≈ 18.2k tokens" under a reference row. */
function RefEstimate({ estimate }: { estimate: TokenEstimate | undefined }) {
  if (!estimate) return null
  const pages = m.knowledge.pages(estimate.pages)
  const tokens = tokensLabel(estimate.tokens, estimate.approximate)
  return (
    <p
      className="px-1 text-xs text-muted-foreground tabular sm:col-span-4"
      title={m.pipelineForm.tokensHint}
    >
      {estimate.whole
        ? m.pipelineForm.wholeDocument(pages, tokens)
        : m.pipelineForm.refEstimate(
            `${m.knowledge.viewer.rangeSummary(estimate.from, estimate.to)} · ${pages}`,
            tokens
          )}
    </p>
  )
}
