# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Bun, React, TanStack (Router, Query, Form, Table), shadcn on Base UI, Tailwind v4. Static build embedded in the Go binary.

## Purpose

Ekin is a self-hosted growth agent for a team's website and channels. It turns ideas, knowledge-base documents and SEO opportunities into YouTube videos and blog articles, audits the website and proposes fixes to its code as pull requests, tracks whether AI answer engines cite the brand, and measures what each published piece did. It holds every piece for human review before anything is published.

It is a tool for content-led and site-led growth, not an editor or a website builder: a team grows its audience by publishing steadily and keeping its site healthy, and Ekin keeps that steady (pipelines on a schedule, a long video cut into shorts, an article beside each video, findings fixed a few at a time). It never hosts the site or migrates it into a CMS; changes reach the site only through the team's own repository. It does not compete with After Effects, Premiere or CapCut, and its surfaces never borrow their timeline-and-clapperboard imagery.

## Users and scene

- Users: small teams anywhere. Content in English (the default) or Turkish.
- Primary scene: a daily review sitting. Someone sits down once or twice a day on a laptop to watch, read or diff the drafts the agent produced, leave feedback (optionally timestamped and scoped to script, voice, visuals, music or metadata), and approve or reject. Everything else runs unattended.
- Secondary: a weekly look at the site: open findings, what merged changes and new articles did in search, and whether AI answers mention the brand.
- Tertiary: checking pipelines, schedules and the job queue when something is slow or failed.
- Roles: owner, editor, viewer.

## Core objects and workflow

- Pipeline: produces stories on a cron schedule or on demand; output video, article, change, or a video and article pair (one lead, the other derived); language en or tr; source is a prompt, knowledge-base references, SEO opportunities, site findings or AI-visibility gaps.
- Story: owns deliverables (video / article / change; lead, derived or translation), each with revisions. Statuses: waiting, queued, drafting, rendering, review, finalizing, ready, publishing, merging, published, rejected, failed.
- Site: the workspace's repository and public URL. Crawled into a page inventory; audit rules raise findings (open, resolved, reopened) with severity and evidence.
- Change: a code or content edit to the site's repository, reviewed as a diff, published as a pull request or a commit.
- Visibility: prompts buyers would ask, asked of several models on a schedule; each answer records mentions, position, cited URLs and competitors.
- Review: every deliverable waits in review; feedback sends it back to the agent for another revision.
- Jobs: background work grouped by the request that started it (agent, tts, render, extract, io queues), with throttle controls and a global pause.
- Publishing: YouTube (private until the API audit passes) and the site's repository via git commit or pull request; a pull request waits in merging until it is merged or closed.
- Measurement: Search Console clicks, impressions and position per page, before and after each published article or change.

## Voice

Plain, specific, calm. Says exactly what the agent is doing and what the person needs to do ("Revision 3 is ready for review", "The agent is writing revision 2…").

## Constraints

- UI copy is English in the current catalogue; content is English or Turkish.
- No invented metrics, customers or claims.
- Never claims a fix worked until the next crawl or the measurement shows it.
