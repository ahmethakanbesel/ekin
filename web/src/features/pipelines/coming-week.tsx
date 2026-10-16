import { useQuery } from "@tanstack/react-query"

import type { Pipeline, UpcomingRun } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { m } from "@/i18n"
import { upcomingRunsQuery } from "@/queries/pipelines"
import { PipelineSwatch } from "./identity"

const DAYS = 7

/** The calendar date of an instant in timeZone, as "YYYY-MM-DD". */
function dayKey(date: Date, timeZone: string): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(
    date,
  )
}

/** Today and the following days in timeZone, by calendar (no DST drift). */
function calendar(now: number, timeZone: string) {
  const [y, mo, d] = dayKey(new Date(now), timeZone).split("-").map(Number)
  const weekday = new Intl.DateTimeFormat(m.locale, { weekday: "short", timeZone: "UTC" })
  return Array.from({ length: DAYS }, (_, i) => {
    const date = new Date(Date.UTC(y, mo - 1, d + i))
    return {
      key: date.toISOString().slice(0, 10),
      day: date.getUTCDate(),
      weekday: weekday.format(date),
      today: i === 0,
    }
  })
}

/**
 * The scheduled runs of the coming seven days, a column per day in the
 * workspace's timezone, so the week's load and its gaps show at a glance.
 */
export function ComingWeek({
  workspaceId,
  pipelines,
  timezone,
  now,
}: {
  workspaceId: string
  pipelines: Pipeline[]
  timezone: string
  now: number
}) {
  const upcoming = useQuery(upcomingRunsQuery(workspaceId, DAYS))
  if (!pipelines.some((p) => p.definition.schedule)) return null

  const time = new Intl.DateTimeFormat(m.locale, {
    timeZone: timezone,
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  })
  const names = new Map(pipelines.map((p) => [p.id, p.definition.name]))
  const days = calendar(now, timezone)
  const byDay = new Map<string, UpcomingRun[]>()
  for (const run of upcoming.data?.runs ?? []) {
    const key = dayKey(new Date(run.at), timezone)
    byDay.set(key, [...(byDay.get(key) ?? []), run])
  }
  // Today's empty column says what already ran.
  const ranToday = pipelines
    .filter((p) => p.lastRunAt && dayKey(new Date(p.lastRunAt), timezone) === days[0].key)
    .toSorted((a, b) => (b.lastRunAt ?? "").localeCompare(a.lastRunAt ?? ""))[0]

  return (
    <Card className="mt-6" aria-labelledby="coming-week-title">
      <CardHeader>
        <CardTitle id="coming-week-title">{m.pipelines.week.title}</CardTitle>
        <CardDescription>{m.pipelines.week.description(timezone)}</CardDescription>
      </CardHeader>
      <CardContent>
        {upcoming.isError ? (
          <div role="alert" className="flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
            {m.pipelines.week.loadFailed}
            <Button variant="outline" size="sm" onClick={() => void upcoming.refetch()}>
              {m.common.retry}
            </Button>
          </div>
        ) : (
          <ol className="grid gap-x-4 gap-y-5 sm:grid-cols-4 lg:grid-cols-7">
            {days.map((day) => {
              const runs = byDay.get(day.key) ?? []
              return (
                <li key={day.key} className="grid content-start gap-2.5">
                  <h3 className="flex items-center gap-2">
                    <span className="text-2xl font-semibold tabular">{day.day}</span>
                    <span className="text-sm text-muted-foreground">{day.weekday}</span>
                    {day.today ? <Badge variant="secondary">{m.pipelines.week.today}</Badge> : null}
                  </h3>
                  {upcoming.isPending ? (
                    <Skeleton className="h-14 w-full" />
                  ) : runs.length ? (
                    <ul className="grid gap-2">
                      {runs.map((run) => (
                        <li
                          key={`${run.pipelineId}-${run.at}`}
                          className="grid gap-0.5 rounded-md border px-3 py-2 text-sm"
                        >
                          <time dateTime={run.at} className="font-mono text-xs text-muted-foreground">
                            {time.format(new Date(run.at))}
                          </time>
                          <span className="flex min-w-0 items-start gap-1.5 font-medium">
                            <PipelineSwatch id={run.pipelineId} className="mt-1.5 size-2 rounded-[2px]" />
                            <span className="min-w-0 break-words">{names.get(run.pipelineId) ?? m.common.none}</span>
                            {run.count > 1 ? (
                              <span className="ml-auto shrink-0 font-normal text-muted-foreground tabular">
                                <span aria-hidden>×{run.count}</span>
                                <span className="sr-only">{m.pipelines.batch(run.count)}</span>
                              </span>
                            ) : null}
                          </span>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p className="text-sm text-muted-foreground">
                      {day.today ? m.pipelines.week.noMoreToday : m.pipelines.week.nothing}
                      {day.today && ranToday?.lastRunAt
                        ? ` ${m.pipelines.week.ranAt(ranToday.definition.name, time.format(new Date(ranToday.lastRunAt)))}`
                        : null}
                    </p>
                  )}
                </li>
              )
            })}
          </ol>
        )}
      </CardContent>
    </Card>
  )
}
