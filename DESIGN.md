---
name: Kurgu
description: A warm, quiet review desk for content pipelines, where the drafts carry the colour and cornflower means a person is needed.
colors:
  cornflower: "oklch(0.8 0.08 250)"
  cornflower-on: "oklch(0.25 0.05 262)"
  cornflower-ink: "oklch(0.5 0.13 262)"
  cornflower-ink-dark: "oklch(0.8 0.08 250)"
  focus-ring: "oklch(0.5 0.13 262)"
  focus-ring-dark: "oklch(0.8 0.08 250)"
  warm-page: "oklch(0.993 0.003 80)"
  warm-page-dark: "oklch(0.155 0.004 70)"
  card-white: "oklch(1 0 0)"
  card-dark: "oklch(0.205 0.005 70)"
  ink: "oklch(0.17 0.006 65)"
  ink-dark: "oklch(0.975 0.003 80)"
  stone-fill: "oklch(0.965 0.004 75)"
  stone-fill-dark: "oklch(0.275 0.005 70)"
  stone-text: "oklch(0.52 0.006 65)"
  stone-text-dark: "oklch(0.72 0.006 70)"
  hairline: "oklch(0.915 0.005 75)"
  hairline-dark: "oklch(1 0 0 / 10%)"
  field-edge: "oklch(0.64 0.006 70)"
  field-edge-dark: "oklch(1 0 0 / 36%)"
  sidebar-linen: "oklch(0.982 0.004 78)"
  sidebar-linen-dark: "oklch(0.185 0.005 70)"
  paper: "oklch(0.975 0.012 80)"
  paper-ink: "oklch(0.22 0.01 60)"
  paper-edge: "oklch(0.87 0.016 75)"
  working-violet: "oklch(0.5 0.15 305)"
  working-violet-dark: "oklch(0.72 0.14 300)"
  ready-green: "oklch(0.38 0.1 168)"
  ready-green-dark: "oklch(0.88 0.15 155)"
  failed-red: "oklch(0.4 0.19 22)"
  failed-red-dark: "oklch(0.68 0.19 22)"
  queue-agent: "oklch(0.58 0.18 295)"
  queue-tts: "oklch(0.34 0.16 350)"
  queue-render: "oklch(0.61 0.1 190)"
  queue-extract: "oklch(0.46 0.14 125)"
  queue-io: "oklch(0.34 0.15 268)"
  pipeline-1: "oklch(0.61 0.1 200)"
  pipeline-2: "oklch(0.55 0.13 40)"
  pipeline-3: "oklch(0.34 0.15 330)"
  pipeline-4: "oklch(0.49 0.13 270)"
  pipeline-5: "oklch(0.34 0.09 130)"
typography:
  headline:
    fontFamily: "Geist Variable, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.5rem"
    fontWeight: 600
    lineHeight: 1.25
    letterSpacing: "-0.025em"
  title:
    fontFamily: "Geist Variable, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.125rem"
    fontWeight: 600
    lineHeight: 1.375
  body:
    fontFamily: "Geist Variable, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 400
    lineHeight: 1.43
  reading:
    fontFamily: "Geist Variable, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.7
  label:
    fontFamily: "Geist Variable, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.75rem"
    fontWeight: 500
    lineHeight: 1.33
  data:
    fontFamily: "Geist Mono Variable, ui-monospace, SFMono-Regular, Menlo, monospace"
    fontSize: "0.75rem"
    fontWeight: 400
    fontFeature: "tnum"
rounded:
  sm: "6px"
  md: "8px"
  lg: "10px"
  xl: "14px"
  pill: "9999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "32px"
components:
  button-primary:
    backgroundColor: "{colors.cornflower}"
    textColor: "{colors.cornflower-on}"
    rounded: "{rounded.md}"
    padding: "0 10px"
    height: "36px"
  button-outline:
    backgroundColor: "{colors.warm-page}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "0 10px"
    height: "36px"
  button-secondary:
    backgroundColor: "{colors.stone-fill}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "0 10px"
    height: "36px"
  button-ghost:
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "0 10px"
    height: "36px"
  button-destructive:
    textColor: "{colors.failed-red}"
    rounded: "{rounded.md}"
    padding: "0 10px"
    height: "36px"
  rail-button:
    rounded: "{rounded.pill}"
    size: "40px"
  rail-approve:
    backgroundColor: "{colors.ready-green}"
    textColor: "{colors.card-white}"
    rounded: "{rounded.pill}"
    size: "40px"
  badge-attention:
    backgroundColor: "{colors.cornflower}"
    textColor: "{colors.cornflower-on}"
    rounded: "{rounded.pill}"
    padding: "2px 8px"
    height: "20px"
  badge-working:
    textColor: "{colors.working-violet}"
    rounded: "{rounded.pill}"
    padding: "2px 8px"
    height: "20px"
  badge-done:
    textColor: "{colors.ready-green}"
    rounded: "{rounded.pill}"
    padding: "2px 8px"
    height: "20px"
  badge-failed:
    textColor: "{colors.failed-red}"
    rounded: "{rounded.pill}"
    padding: "2px 8px"
    height: "20px"
  badge-waiting:
    textColor: "{colors.ink}"
    rounded: "{rounded.pill}"
    padding: "2px 8px"
    height: "20px"
  input:
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "4px 10px"
    height: "36px"
  card:
    backgroundColor: "{colors.card-white}"
    textColor: "{colors.ink}"
    rounded: "{rounded.xl}"
    padding: "24px"
  poster:
    rounded: "{rounded.xl}"
  tab-list:
    backgroundColor: "{colors.stone-fill}"
    textColor: "{colors.stone-text}"
    rounded: "{rounded.lg}"
    padding: "3px"
    height: "36px"
  tab-active:
    backgroundColor: "{colors.warm-page}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
  toggle-pressed:
    backgroundColor: "{colors.stone-fill}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    height: "36px"
---

# Design System: Kurgu

## Overview

**Creative North Star: "The Quiet Review Desk"**

Kurgu is a desk where an operator sits down to judge what the agent made. The desk itself stays warm, near-white and quiet: off-white paper tones with a faint amber cast (hue 65 to 80 throughout the neutrals), hairline borders, stock shadcn geometry, one sans family. Everything loud on screen belongs to the work under review: video poster frames, article sheets, a pipeline's identity hue behind a poster. The chrome steps back so the content can carry the strongest colour.

One colour speaks for the system: cornflower. It is spent on exactly three jobs: something needs you (an output in review, the review count, an unread notification, the revision awaiting a decision), the one primary action of a surface, and keyboard focus. Selection, navigation state and decoration stay neutral. Status reads through a small fixed vocabulary of tones (working violet, ready green, failed red, neutral waiting, struck-through muted rejection), always paired with a word or icon, never colour alone.

The system is stock shadcn (base-vega style on Base UI) with a short list of deliberate local deviations, every one of them for accessibility: full-strength focus rings, a 3:1 neutral edge on pressed toggles and selected tabs, a darker cornflower boundary on every cornflower fill, and touch-area extensions on small buttons. Both themes are first-class and both meet WCAG 2.2 AA.

**Key Characteristics:**
- Warm near-neutral chrome in both themes; content carries the strongest colour.
- Cornflower only for needs-you, the primary action, and focus.
- Status tones from one table (`lib/tones.ts`), always with a text or icon cue.
- Stock shadcn base-vega components; deviations exist only to meet AA.
- Geist and Geist Mono, one family, weight and size doing the hierarchy.
- Drafts are judged at their real shape: 9:16, 16:9, square, or a sheet of paper.

## Colors

A warm off-white desk with a single cornflower signal, three status tones, and two small categorical hue sets (queues and pipelines) that live only in dots, swatches and bars.

### Primary
- **Cornflower** (cornflower): the needs-you signal and the primary action. Fills the primary button, the attention badge ("In review"), the review-count pill in the sidebar and tab list, the unread notification count, and the feedback sheet's send button. Identical in both themes. As a fill it sits at 1.82:1 against the light page, so it never stands alone: every cornflower fill carries a 1px inset boundary in Cornflower Ink.
- **Cornflower On** (cornflower-on): the deep navy text on any cornflower fill (8.6:1).
- **Cornflower Ink** (cornflower-ink, cornflower-ink-dark): cornflower as text, edge and dot. Used for cornflower boundaries, the review count beside the Review nav item, the attention banner icon, "request changes" on the action rail, the in-review revision label, link text in rendered Markdown, the text caret, and small "needs a look" dots (fresh notification, paused throttle). 6.0:1 on the light page; in dark it equals Cornflower itself (10.5:1).
- **Focus Ring** (focus-ring, focus-ring-dark): Cornflower Ink in light (6.0:1) and Cornflower in dark. It is the only ring colour; see Components.

### Neutral
- **Warm Page** (warm-page / warm-page-dark): the app background.
- **Card White** (card-white / card-dark): cards, popovers, sheets; one step off the page in each theme.
- **Ink** (ink / ink-dark): all primary text, 18.8:1 light and 18.2:1 dark.
- **Stone Fill** (stone-fill / stone-fill-dark): secondary, muted and accent surfaces: tab-list track, pressed toggle fill, secondary buttons, code blocks, hover rows.
- **Stone Text** (stone-text / stone-text-dark): secondary text, metadata, counts, timestamps. 5.4:1 light, 7.9:1 dark; it is body-legible, not a faint grey.
- **Hairline** (hairline / hairline-dark): borders and dividers. Decorative only; it never carries state.
- **Field Edge** (field-edge / field-edge-dark): input and unchecked-control borders, 3.3:1 so a field's boundary is visible.
- **Sidebar Linen** (sidebar-linen / sidebar-linen-dark): the sidebar, a shade off the page.

### Paper
- **Paper / Paper Ink / Paper Edge**: the `.paper` scope. Article drafts and article posters are a light, slightly warmer sheet in both themes (it re-scopes card, foreground, muted, border, input, Cornflower Ink and ring, and forces `color-scheme: light`). An article reads as a printed page even in the dark theme.

### Status
- **Working Violet** (working-violet / -dark): the system is working: drafting, rendering, finalizing, publishing, running jobs, the planning banner.
- **Ready Green** (ready-green / -dark): done and good: ready, approved, published, succeeded, an active pipeline, the approve button.
- **Failed Red** (failed-red / -dark): failed and the reject action.

Badges lay a status tone over a tint of itself (working and done at 15%, failed at 10% light / 15% dark); banners use a 5% tint with a 30% edge and a tone-coloured leading icon.

### Categorical
- **Queue hues** (queue-agent, queue-tts, queue-render, queue-extract, queue-io; theme chart-1..5): one hue per worker queue, used as dots, queue badges and the segments of a job's time bar. The control queue is plumbing and takes Stone Text. A job phase takes its queue's hue (plan and other on control, draft/seo/template on agent, voice on tts, render and final on render, publish on io). Queued time is a Stone Text hatch, idle time Stone Text at 10%.
- **Pipeline hues** (pipeline-1..5): a pipeline's identity, hashed from its id (31-multiplier string hash, modulo 5); there is no colour field. Shown as a small square swatch beside the pipeline name and, darkened into a radial gradient, as the backdrop of that pipeline's video posters. Ad-hoc stories take Stone Text.

Every queue and pipeline hue clears 3:1 against the page in both themes (lowest: 3.5:1 light, 4.7:1 dark).

### Named Rules
**The Needs-You Rule.** Cornflower means a person is needed, the surface's one primary action, or focus. A selected tab, the active nav item, a pressed filter, a hover, a decorative accent: neutral, always.

**The Ink Boundary Rule.** Cornflower is never a bare fill. Any cornflower surface carries an inset 1px Cornflower Ink edge; any cornflower text or edge uses Cornflower Ink, not Cornflower.

**The Loud Content Rule.** The strongest colour on a review screen comes from the draft (poster, video, article sheet, pipeline backdrop), not from the chrome.

**The Never Alone Rule.** No state rests on colour alone: status badges carry words, banners carry icons, rejected outputs are struck through, queue and pipeline swatches sit beside their names.

## Typography

**Display Font:** none; there is no display face.
**Body Font:** Geist Variable (with ui-sans-serif, system-ui)
**Label/Mono Font:** Geist Mono Variable (with ui-monospace, SFMono-Regular, Menlo)

**Character:** One neutral grotesque for everything; hierarchy comes from weight (400 / 500 / 600) and a short size ramp, never from a second family. Mono appears only where characters must line up: timestamps, queue names, hex values, tokens.

### Hierarchy
- **Headline** (600, 1.5rem, tight tracking): page titles in the page header, and the article title in the feed.
- **Title** (600, 1.125rem, snug): a draft's title under its frame, sheet and dialog titles, empty-state headings. Poster titles step down to 0.875rem semibold over the poster foot.
- **Body** (400, 0.875rem): the working size of the app: lists, forms, metadata, descriptions.
- **Reading** (400, 1rem, 1.7 line height): rendered Markdown for articles and scripts. Headings inside it are 600 with -0.01em tracking at 1.6 / 1.3 / 1.1em; in the compact script column beside the feed, headings mark scenes and shrink to 1.1em and 1em.
- **Label** (500, 0.75rem): badges, action-rail captions, counts, small metadata.
- **Data** (Geist Mono, tabular figures): timestamps such as 0:21, queue badges, durations. Counts that change in place use tabular figures in the sans.

### Named Rules
**The One Family Rule.** Geist carries every role. Hierarchy is weight and size; a second family or a display face is out of system.

**The Sentence Case Rule.** Headings, block labels, tabs and buttons are sentence case. Uppercase is reserved for data that is uppercase by nature (language codes, hex values, initials).

## Layout

The shell is the stock shadcn sidebar: a collapsible left rail (icon-only when collapsed) with the workspace switcher on top, the nav in the middle and the user at the foot; a 48px sticky header holds the sidebar trigger, the breadcrumb, notifications and the theme toggle. Below `md` the sidebar gives way to a horizontally scrolling nav strip under the header. Content sits in a centred column up to 1280px with 16px padding on phones and 32px from `md`. The story page opts out and lays its feed and panels edge to edge.

Spacing runs on the 4px Tailwind step: 4 and 8 inside controls, 12 to 16 between related items, 24 between sections (page header to content, tabs to grid), 32 between rows of posters.

**The review inbox** is a poster grid: two columns, four from `lg`, 16px gutters (24px from `lg`) and 32px between rows. Every poster is 9:16 regardless of the draft's own shape.

**The story feed** is a vertical snap scroller of drafts, one per snap, with the next draft peeking below (72px, 40px on phones). Every draft shares one column, the width of a 9:16 frame as tall as the feed allows, from one left edge. A 16:9 draft grows rightward from that edge; a 9:16 draft carries its caption strip over its foot; an article is a scrolling paper sheet at the 9:16 size. An 88px action rail stands beside the frame (16px gap); under 640px the rail drops below the frame as a row. Beside the feed, a dockable feedback sheet and a script-and-history panel fill the remaining width on wide screens.

**Jobs** pairs the list with the throttle panel side by side from 1440px; below that the throttle opens from a button.

### Named Rules
**The Real Shape Rule.** A draft is shown at its delivered aspect (9:16, 16:9, square, or a page), never cropped to fit a card.

## Elevation & Depth

Flat by default, with stock shadcn's small shadows. Depth comes from tonal steps (page, card, sidebar, stone fill) and hairline borders. Shadows are light and functional: extra-small under buttons, inputs and cards (cards pair theirs with a 10% ink ring); small under posters and draft frames, rising to medium on poster hover; large only for overlays (sheets, dropdowns, popovers). Floating strips over media (poster foot, caption strip, format badges) use the page colour at 80 to 85% so the draft shows through without hurting legibility.

### Named Rules
**The Tonal Depth Rule.** A surface is distinguished by its fill and hairline first. A shadow larger than small belongs only to something that floats above the page.

## Shapes

Gently rounded throughout, from one base radius of 10px: controls at 8px, cards, posters and draft frames at 14px, badges and rail buttons fully round. Small buttons cap their radius at 8 to 10px. Swatches are tiny squares with 2 to 3px corners so they read as identity marks, while queue dots are circles. Hairline borders define containers; the 3px Markdown blockquote rule is the only heavier stroke.

## Components

The base is stock shadcn base-vega on Base UI. Only the deviations below are local; everything else should be regenerated from the stock style, not hand-edited.

### Buttons
- **Shape:** 8px corners, 36px tall by default (24 / 32 / 40px sizes), 14px medium label, icons at 16px.
- **Primary:** Cornflower with Cornflower On text and an inset 1px Cornflower Ink boundary; hover drops to 80% fill. One per surface: "Start review sitting", "Send 3 notes to the agent" in the feedback sheet.
- **Outline / Secondary / Ghost:** page fill with hairline border; stone fill; transparent with stone hover. These carry all ordinary actions.
- **Destructive:** red text on a 10% red tint (20% in dark).
- **Link:** Cornflower Ink text, underline on hover.
- **Focus:** a 3px ring in Focus Ring at full strength (stock uses 50%).
- **Press:** a 1px downward nudge.
- **Touch:** extra-small and small icon buttons extend their hit area with an invisible inset (8px, 6px) below `md`.

### Action Rail
- **Character:** round 40px buttons in a column beside the draft, each with a 12px medium caption beneath; the caption is visual, the button carries the accessible name.
- **Approve:** Ready Green fill with light text. **Request changes:** Cornflower Ink edge and text with a 10% cornflower hover. **Revisions / Open publication:** neutral outline. **Reject:** red edge and text, separated from the rest by a hairline divider and given a red caption.
- **Revisions** shows the revision on screen as a small secondary badge at its corner; **Request changes** shows the count of unsent notes as a Cornflower badge at its corner while the sheet is closed.
- **Request changes** is the only way into the feedback sheet, and the **N** key does the same thing: pause the video and open the sheet on a new note pinned at the playhead. Its tooltip names the key. Viewers cannot send feedback, so they get no feedback control; earlier notes are in History for everyone.
- **Approving** waits six seconds before it is sent: the toast names the story with an Undo action and a green bar draining over the window, and the Approve button turns into a green-edged Undo with a ring draining around it (both hidden when motion is reduced). The feed moves on at the click; a tab closing sends what is waiting.

### Feedback Sheet
- **Opening:** closed until there is something to say. It opens on Request changes, on N, and by itself on a draft that still has unsent notes. From 1280px it docks between the feed and the panel and stays as the reviewer leaves it from draft to draft and for the browser session (session storage; it works without it); sending closes it. Below that it is a sheet over the page (from the bottom on phones) and never opens by itself. Esc or the close button closes it and focus returns to Request changes.
- **Character:** a review round is a list of notes, sent together. The form on top writes one note (scope, timestamp for videos, text); "Add note" (with its ⌘/Ctrl Enter hint, hidden on touch) moves it to "Notes to send", a bordered list with hairline dividers, untimed notes first and then by time, each showing time, scope and text. A listed note opens back in the form when clicked (the note being written takes its place) and has its own remove button.
- **Sending:** the footer's primary button counts what it sends, the typed note included: "Send note to the agent", "Send 3 notes to the agent". Under it, a line names the N key.
- **Unsent notes** live with the draft until sent, through closing the sheet and moving through the feed. Approving or rejecting with notes waiting asks first.
- **Earlier notes** are not repeated in the sheet; History in the panel is the record. The sheet says how many there are in one line, "2 earlier notes · see History", and the link brings History into view (closing the sheet first below 1280px).

### Badges and Status
- **Style:** fully round, 20px tall, 12px medium label.
- **Tones:** attention is solid Cornflower with the ink boundary (only for "In review"); working, done and failed are tinted; waiting and queued are outlined with Ink text; rejected and cancelled are outlined with Stone Text, and a rejected output is also struck through.
- **Mapping:** waiting, queued: neutral. drafting, rendering, finalizing, publishing, running: working. review: attention. ready, published, succeeded: done. failed: failed. rejected, cancelled: muted. Pipelines: active is done with a check icon, paused is neutral with a pause icon. Running jobs spin their icon.

### Banners
- **Style:** a bordered callout with a leading icon in the tone colour: working (5% violet tint, 30% violet edge), attention (10% cornflower tint, 40% Cornflower Ink edge), done, failed, muted.
- **Failed:** leads with what failed in plain words and, for editors, whether Retry is likely to help, phrased as guidance; the raw error sits in a closed "Technical details" disclosure in small mono.

### Tabs and Toggles
- **Tab list:** stone track, 10px corners, 3px inset, 36px tall. Inactive tabs are Ink at 60% (Stone Text in dark).
- **Selected tab:** page fill, Ink text, and a 1px inset edge in Ink at 70% (6.6:1 light, 8.1:1 dark against the list) so selection never rests on a 1.1:1 fill. Counts sit beside the label in Stone Text; only the review count is a Cornflower pill.
- **Pressed toggle:** stone fill, Ink text, the same 1px Ink-at-70% inset edge (7.5:1 light, 9.9:1 dark). Revision toggles use Ink at 60%, except the revision waiting for a decision, which takes the Cornflower Ink edge and text on a 10% cornflower tint.

### Cards / Containers
- **Corner Style:** 14px.
- **Background:** Card White with a 10% ink ring and an extra-small shadow.
- **Internal Padding:** 24px (16px in the small size).
- **Lists** (the jobs list) are a single bordered 14px container with hairline dividers between rows and a 40% stone hover.

### Posters
- **Character:** the inbox shows each story as a 9:16 poster of its lead output: the video's poster frame over a pipeline-hue gradient, or the article as a paper sheet. A format badge sits top left; the lead's status badge and title sit in a strip over the foot on the page colour at 85%. Under the poster: output lines, the failure if any (red, with a plain sentence of what failed in Stone Text, never the raw error), origin with the pipeline swatch, language badge and relative age.
- **Hover / Focus:** shadow rises from small to medium; focus draws the 3px ring around the whole poster.

### Player
- **Character:** the video frame is always dark-scoped with a black stage. The caption strip under it holds status, format and language badges, the title, origin and tags, then the scrubber and the "feedback at" timestamps in mono.
- **Scrubber:** a 6px round track with a field-edge inset outline, a white 16px thumb, and small round markers where feedback notes were pinned: hollow for notes the agent has, filled for the reviewer's unsent ones. The times listed beside it are the sent notes only. The thumb exposes position and value text to assistive tech.

### Inputs / Fields
- **Style:** transparent field, 1px Field Edge border, 8px corners, 36px tall, extra-small shadow; 30% field-edge fill in dark. Caret in Cornflower Ink.
- **Focus:** Focus Ring border plus a 3px full-strength ring.
- **Error:** red border with a 20% red ring.
- **Slider:** stock with a white thumb edged in Cornflower Ink, plus an optional thumb label and value text for screen readers.

### Navigation
- **Sidebar:** stock shadcn sidebar on Sidebar Linen; the active item takes the stone accent fill and medium weight, never cornflower. The review count is a Cornflower Ink number.
- **Phone nav strip:** 32px items in Stone Text; active on stone fill with Ink text; the review count is a Cornflower pill.
- **Brand mark:** a 32px cornflower square with a clapperboard and the ink boundary; it is the identity asset, not a pattern for other surfaces.

## Do's and Don'ts

### Do:
- **Do** use Cornflower only for something that needs a person, the one primary action of a surface, and focus.
- **Do** give every cornflower fill an inset 1px Cornflower Ink edge, and use Cornflower Ink for cornflower text, edges and dots.
- **Do** mark selected tabs and pressed toggles neutrally: stone or page fill plus a 1px Ink-at-70% inset edge (at least 3:1).
- **Do** take status colour from `lib/tones.ts`: working violet, ready green, failed red, neutral for waiting and queued, Stone Text plus strikethrough for rejected.
- **Do** colour queues by the chart-1..5 hues (agent, tts, render, extract, io) with control in Stone Text, and pipelines by their hashed pipeline-1..5 hue.
- **Do** pair every colour cue with a word, icon or strikethrough.
- **Do** keep focus rings at full strength (3px, Focus Ring) on every focusable element, including posters, feed and links.
- **Do** check new colours in both themes: 4.5:1 for text, 3:1 for boundaries, state edges and focus.
- **Do** show drafts at their real aspect and let them carry the strongest colour on screen.
- **Do** regenerate UI primitives from the stock base-vega style and re-apply only the documented deviations.

### Don't:
- **Don't** use Cornflower for a selected tab, the active nav item, a pressed filter, hover, or decoration.
- **Don't** put a bare Cornflower fill or Cornflower text on the light page; it is 1.82:1.
- **Don't** let a state rest on a tinted fill alone; the stone fill is about 1.1:1 against the page.
- **Don't** invent metrics, scores or counts the API does not return.
- **Don't** add a second type family, a display face, or uppercase labels for headings or sections.
- **Don't** assign queue or pipeline hues by hand or reuse them for status.
- **Don't** soften focus rings back to the stock 50%.
