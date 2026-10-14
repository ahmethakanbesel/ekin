import { Loader2Icon } from "lucide-react"

import type { DocumentFormat, DocumentStatus } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"

const statusTone: Record<DocumentStatus, string> = {
  pending: "bg-sky-500/15 text-sky-700 dark:text-sky-300",
  ready: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
  failed: "bg-destructive/10 text-destructive",
}

export function StatusBadge({ status }: { status: DocumentStatus }) {
  return (
    <Badge variant="secondary" className={cn(statusTone[status])}>
      {status === "pending" ? <Loader2Icon data-icon="inline-start" className="animate-spin" /> : null}
      {m.knowledge.status[status]}
    </Badge>
  )
}

export function FormatBadge({ format }: { format: DocumentFormat }) {
  return (
    <Badge variant="outline" className="font-mono text-[11px] tracking-wide">
      {m.values.documentFormat[format] ?? format}
    </Badge>
  )
}
