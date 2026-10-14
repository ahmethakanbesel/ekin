import type { ReactNode } from "react"
import { useForm, useStore, type DeepKeys } from "@tanstack/react-form"
import { ArrowDownIcon, ArrowUpIcon, PlusIcon, XIcon } from "lucide-react"

import type { TemplateFont, TemplateInput } from "@/api/client"
import { Button } from "@/components/ui/button"
import { FieldGroup } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { Row, Section, shownError } from "@/features/pipelines/form-parts"
import { Markdown } from "@/features/stories/markdown"
import { m } from "@/i18n"
import { formatInteger } from "@/lib/format"
import { cn } from "@/lib/utils"
import { KindsCheckboxes } from "./kinds-checkboxes"
import {
  FONTS,
  HEX,
  TEMPLATE_LIMITS,
  bodyStyle,
  headingStyle,
  toTemplateInput,
  validateTemplate,
  type TemplateFormValues,
} from "./model"
import { PaletteStrip, TitleCard } from "./preview"

type Props = {
  initialValues: TemplateFormValues
  submitLabel: string
  /** Where Cancel goes. */
  cancel: ReactNode
  /** Shown above the actions: the server's rejection, if any. */
  banner?: ReactNode
  onSubmit: (input: TemplateInput) => Promise<void>
}

const t = m.templates.form

export function TemplateForm({ initialValues, submitLabel, cancel, banner, onSubmit }: Props) {
  const form = useForm({
    defaultValues: initialValues,
    validators: {
      onChange: ({ value }) => {
        const fields = validateTemplate(value)
        if (Object.keys(fields).length === 0) return undefined
        return { fields: fields as Partial<Record<DeepKeys<TemplateFormValues>, string>> }
      },
    },
    // Colours added since the last validation may have lost their errors
    // on remount; validate now so every error shows.
    onSubmitInvalid: ({ formApi }) => void formApi.validate("change"),
    onSubmit: async ({ value }) => {
      await onSubmit(toTemplateInput(value))
    },
  })

  const submitted = useStore(form.store, (s) => s.submissionAttempts > 0)
  const name = useStore(form.store, (s) => s.values.name)
  const palette = useStore(form.store, (s) => s.values.palette)
  const headingFont = useStore(form.store, (s) => s.values.headingFont)
  const bodyFont = useStore(form.store, (s) => s.values.bodyFont)
  const shown = (meta: { isTouched: boolean; errors: unknown[] }) => shownError(meta, submitted)

  return (
    <form
      noValidate
      className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]"
      onSubmit={(e) => {
        e.preventDefault()
        void form.handleSubmit()
      }}
    >
      <div className="grid min-w-0 gap-6">
        <Section title={t.sections.basics}>
          <FieldGroup className="gap-5">
            <form.Field name="name">
              {(field) => (
                <Row label={t.name} htmlFor="tpl-name" error={shown(field.state.meta)}>
                  <Input
                    id="tpl-name"
                    autoComplete="off"
                    maxLength={TEMPLATE_LIMITS.name + 20}
                    placeholder={t.namePlaceholder}
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    onBlur={field.handleBlur}
                    aria-invalid={!!shown(field.state.meta) || undefined}
                  />
                </Row>
              )}
            </form.Field>
            <form.Field name="description">
              {(field) => (
                <Row
                  label={t.description}
                  htmlFor="tpl-description"
                  hint={t.descriptionHint}
                  error={shown(field.state.meta)}
                >
                  <Textarea
                    id="tpl-description"
                    rows={2}
                    className="min-h-16"
                    placeholder={t.descriptionPlaceholder}
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    onBlur={field.handleBlur}
                    aria-invalid={!!shown(field.state.meta) || undefined}
                  />
                </Row>
              )}
            </form.Field>
            <form.Field name="inspiration">
              {(field) => (
                <Row
                  label={t.inspiration}
                  htmlFor="tpl-inspiration"
                  hint={t.inspirationHint}
                  error={shown(field.state.meta)}
                >
                  <Input
                    id="tpl-inspiration"
                    autoComplete="off"
                    placeholder={t.inspirationPlaceholder}
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    onBlur={field.handleBlur}
                    aria-invalid={!!shown(field.state.meta) || undefined}
                  />
                </Row>
              )}
            </form.Field>
            <form.Field name="kinds">
              {(field) => (
                <Row label={t.kinds} hint={t.kindsHint} error={shown(field.state.meta)}>
                  <KindsCheckboxes
                    idPrefix="tpl-kind"
                    value={field.state.value}
                    onChange={(next) => {
                      field.handleChange(next)
                      field.handleBlur()
                    }}
                    invalid={!!shown(field.state.meta)}
                  />
                </Row>
              )}
            </form.Field>
          </FieldGroup>
        </Section>

        <Section title={t.sections.look} description={t.lookHint}>
          <FieldGroup className="gap-6">
            <form.Field name="palette" mode="array">
              {(field) => {
                const colours = field.state.value
                const listError = shown(field.state.meta)
                return (
                  <Row label={t.palette} hint={t.paletteHint} error={listError}>
                    {colours.length === 0 ? (
                      <p className="text-sm text-muted-foreground">{t.noColours}</p>
                    ) : (
                      <ol className="grid gap-2">
                        {colours.map((_, i) => (
                          <form.Field key={i} name={`palette[${i}]`}>
                            {(sub) => {
                              const error = shown(sub.state.meta)
                              const value = sub.state.value
                              const n = i + 1
                              return (
                                <li className="grid gap-1">
                                  <div className="flex items-center gap-2">
                                    <input
                                      type="color"
                                      aria-label={t.colour(n)}
                                      className="size-9 shrink-0 cursor-pointer rounded-xl border bg-transparent p-0.5 [&::-moz-color-swatch]:rounded-lg [&::-moz-color-swatch]:border-none [&::-webkit-color-swatch]:rounded-lg [&::-webkit-color-swatch]:border-none [&::-webkit-color-swatch-wrapper]:p-0"
                                      value={HEX.test(value) ? value.toLowerCase() : "#000000"}
                                      onChange={(e) => sub.handleChange(e.target.value.toUpperCase())}
                                    />
                                    <Input
                                      aria-label={t.hex(n)}
                                      className="w-32 font-mono uppercase"
                                      spellCheck={false}
                                      autoComplete="off"
                                      maxLength={7}
                                      value={value}
                                      onChange={(e) => {
                                        const raw = e.target.value.trim()
                                        sub.handleChange(
                                          raw && !raw.startsWith("#") ? `#${raw}` : raw
                                        )
                                      }}
                                      onBlur={() => {
                                        sub.handleChange(sub.state.value.toUpperCase())
                                        sub.handleBlur()
                                      }}
                                      aria-invalid={!!error || undefined}
                                    />
                                    <div className="ml-auto flex items-center gap-0.5">
                                      <Button
                                        type="button"
                                        variant="ghost"
                                        size="icon-sm"
                                        aria-label={t.moveUp(n)}
                                        title={t.moveUp(n)}
                                        disabled={i === 0}
                                        onClick={() => field.swapValues(i, i - 1)}
                                      >
                                        <ArrowUpIcon />
                                      </Button>
                                      <Button
                                        type="button"
                                        variant="ghost"
                                        size="icon-sm"
                                        aria-label={t.moveDown(n)}
                                        title={t.moveDown(n)}
                                        disabled={i === colours.length - 1}
                                        onClick={() => field.swapValues(i, i + 1)}
                                      >
                                        <ArrowDownIcon />
                                      </Button>
                                      <Button
                                        type="button"
                                        variant="ghost"
                                        size="icon-sm"
                                        aria-label={t.removeColour(n)}
                                        title={t.removeColour(n)}
                                        onClick={() => field.removeValue(i)}
                                      >
                                        <XIcon />
                                      </Button>
                                    </div>
                                  </div>
                                  {error ? (
                                    <p role="alert" className="text-sm text-destructive">
                                      {error}
                                    </p>
                                  ) : null}
                                </li>
                              )
                            }}
                          </form.Field>
                        ))}
                      </ol>
                    )}
                    <div>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={colours.length >= TEMPLATE_LIMITS.palette}
                        onClick={() => field.pushValue(colours.at(-1) ?? "#FFFFFF")}
                      >
                        <PlusIcon data-icon="inline-start" />
                        {t.addColour}
                      </Button>
                    </div>
                  </Row>
                )
              }}
            </form.Field>

            <div className="grid gap-5 sm:grid-cols-2">
              <form.Field name="headingFont">
                {(field) => (
                  <Row label={t.headingFont} htmlFor="tpl-heading-font">
                    <FontSelect
                      id="tpl-heading-font"
                      value={field.state.value}
                      onChange={field.handleChange}
                      look={headingStyle}
                    />
                  </Row>
                )}
              </form.Field>
              <form.Field name="bodyFont">
                {(field) => (
                  <Row label={t.bodyFont} htmlFor="tpl-body-font">
                    <FontSelect
                      id="tpl-body-font"
                      value={field.state.value}
                      onChange={field.handleChange}
                      look={bodyStyle}
                    />
                  </Row>
                )}
              </form.Field>
            </div>

            <form.Field name="musicMood">
              {(field) => (
                <Row
                  label={t.musicMood}
                  htmlFor="tpl-music"
                  hint={t.musicMoodHint}
                  error={shown(field.state.meta)}
                >
                  <Input
                    id="tpl-music"
                    autoComplete="off"
                    placeholder={t.musicMoodPlaceholder}
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    onBlur={field.handleBlur}
                    aria-invalid={!!shown(field.state.meta) || undefined}
                  />
                </Row>
              )}
            </form.Field>
          </FieldGroup>
        </Section>

        <Section title={t.sections.guide}>
          <form.Field name="guide">
            {(field) => {
              const error = shown(field.state.meta)
              const count = [...field.state.value.trim()].length
              return (
                <Row label={t.guide} htmlFor="tpl-guide" error={error}>
                  <Tabs defaultValue="write" className="gap-3">
                    <TabsList>
                      <TabsTrigger value="write" className="flex-none">
                        {t.write}
                      </TabsTrigger>
                      <TabsTrigger value="preview" className="flex-none">
                        {t.preview}
                      </TabsTrigger>
                    </TabsList>
                    <TabsContent value="write" keepMounted>
                      <Textarea
                        id="tpl-guide"
                        rows={22}
                        spellCheck={false}
                        className="min-h-[28rem] font-mono text-[13px] leading-relaxed md:text-[13px]"
                        value={field.state.value}
                        onChange={(e) => field.handleChange(e.target.value)}
                        onBlur={field.handleBlur}
                        aria-invalid={!!error || undefined}
                        aria-describedby="tpl-guide-count"
                      />
                    </TabsContent>
                    <TabsContent value="preview">
                      <div className="min-h-[28rem] rounded-3xl border px-5 py-4">
                        {field.state.value.trim() ? (
                          <Markdown source={field.state.value} className="max-w-[72ch] text-[15px]" />
                        ) : (
                          <p className="text-muted-foreground">{t.previewEmpty}</p>
                        )}
                      </div>
                    </TabsContent>
                  </Tabs>
                  <div className="flex flex-wrap justify-between gap-2 text-sm text-muted-foreground">
                    {error ? null : <span>{t.guideHint}</span>}
                    <span
                      id="tpl-guide-count"
                      className={cn(
                        "ml-auto tabular",
                        count > TEMPLATE_LIMITS.guide && "font-medium text-destructive"
                      )}
                    >
                      {t.guideCount(formatInteger(count), formatInteger(TEMPLATE_LIMITS.guide))}
                    </span>
                  </div>
                </Row>
              )
            }}
          </form.Field>
        </Section>

        {banner}

        <div className="flex flex-wrap justify-end gap-2">
          {cancel}
          <form.Subscribe selector={(s) => s.isSubmitting}>
            {(submitting) => (
              <Button type="submit" disabled={submitting}>
                {submitting ? m.common.saving : submitLabel}
              </Button>
            )}
          </form.Subscribe>
        </div>
      </div>

      <aside className="order-first lg:sticky lg:top-8 lg:order-none" aria-label={t.livePreview}>
        <div className="overflow-hidden rounded-4xl bg-card shadow-md ring-1 ring-foreground/5 dark:ring-foreground/10">
          <TitleCard template={{ name, palette, headingFont }} />
          <div className="grid gap-3 p-5">
            <PaletteStrip palette={palette} />
            <p className="text-sm" style={bodyStyle(bodyFont)}>
              {m.templates.sampleBody}
            </p>
          </div>
        </div>
      </aside>
    </form>
  )
}

const fontItems = FONTS.map((f) => ({ value: f, label: f }))

function FontSelect({
  id,
  value,
  onChange,
  look,
}: {
  id: string
  value: TemplateFont
  onChange: (font: TemplateFont) => void
  look: (font: TemplateFont) => { fontFamily: string; fontWeight: number }
}) {
  return (
    <Select items={fontItems} value={value} onValueChange={(v) => v && onChange(v as TemplateFont)}>
      <SelectTrigger id={id} className="w-full">
        <SelectValue>
          {(v: TemplateFont) => (
            <span className="text-base" style={look(v)}>
              {v}
            </span>
          )}
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        {FONTS.map((f) => (
          <SelectItem key={f} value={f}>
            <span className="text-base" style={look(f)}>
              {f}
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
