# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Bun, React, TanStack (Router, Query, Form, Table), shadcn on Base UI, Tailwind v4. Static build embedded in the Go binary.

## Purpose

Ekin turns ideas, knowledge-base documents and SEO opportunities into YouTube videos and blog articles, renders them unattended, and holds every piece for human review before anything is published.

It is a tool for content-led growth, not an editor: a team grows its audience by publishing steadily, and Ekin keeps that steady (pipelines on a schedule, a long video cut into shorts, an article beside each video). It does not compete with After Effects, Premiere or CapCut, and its surfaces never borrow their timeline-and-clapperboard imagery.

## Users and scene

- First user: a small team. Turkish first, English second.
- Primary scene: a daily review sitting. Someone sits down once or twice a day on a laptop to watch or read the drafts the agent produced, leave feedback (optionally timestamped and scoped to script, voice, visuals, music or metadata), and approve or reject. Everything else runs unattended.
- Secondary: checking pipelines, schedules and the job queue when something is slow or failed.
- Roles: owner, editor, viewer.

## Core objects and workflow

- Pipeline: produces stories on a cron schedule or on demand; output video, article or both (one lead, the other derived); language tr or en; source is a prompt, knowledge-base references, or SEO opportunities.
- Story: owns deliverables (video / article; lead, derived or translation), each with revisions. Statuses: waiting, queued, drafting, rendering, review, finalizing, ready, publishing, published, rejected, failed.
- Review: every deliverable waits in review; feedback sends it back to the agent for another revision.
- Jobs: background work grouped by the request that started it (agent, tts, render, extract, io queues), with throttle controls and a global pause.
- Publishing: YouTube (private until the API audit passes) and the marketing site's blog via git commit.

## Voice

Plain, specific, calm. Says exactly what the agent is doing and what the person needs to do ("Revision 3 is ready for review", "The agent is writing revision 2…").

## Constraints

- UI copy is English in the current catalogue; content is often Turkish.
- No invented metrics, customers or claims.
