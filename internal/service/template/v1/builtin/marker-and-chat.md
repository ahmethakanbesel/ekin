---
name: Marker and chat
description: Cream paper, a soft serif with a lime marker swipe, chat bubbles over warm photos and app windows; friendly product storytelling.
inspiration: Spacefast, the publishing layer for agents (spacefast.com)
kinds: [video, article]
palette: ["#F7F5EF", "#0C0C0C", "#D4F25A", "#0A84FF", "#E9E9EB", "#FFFFFF", "#6E6E73", "#FF5F57"]
headingFont: Fraunces
bodyFont: Inter
musicMood: Light lo-fi groove with soft keys and brushed drums; friendly and a little playful.
---
## Look
- Cream background (#F7F5EF), ink (#0C0C0C) type, white cards with a hairline border (#0C0C0C at 8%) and 18 px radius. Lots of space; one card or one photo per beat.
- The marker: the key phrase of each title gets a lime (#D4F25A) highlighter swipe behind it, drawn as an SVG path with rough, slightly tilted ends (not a clean rectangle).
- Chat: conversations as message bubbles, incoming in grey (#E9E9EB, ink text), outgoing in blue (#0A84FF, white text), each with a tiny name above ("Sam", "You") and the odd emoji reaction badge. Short, lowercase, real-sounding lines.
- Photos: warm, dim, top-down scenes (a desk at night, a lamp, a notebook) from stock, in a 28 px rounded frame, with chat bubbles floating over them.
- App windows: simple macOS-style windows drawn in HTML: a grey title bar with traffic lights (#FF5F57, #FEBC2E, #28C840), a centred title, and a plain list or form inside (a version history, a toggle list, a table). One row is highlighted in solid ink with white text.
- Doodles: little hand-drawn line figures and arrows (SVG, 2 px round strokes, ink) sitting on the corners of cards, as if someone sketched in the margin.

## Typography
- Titles in Fraunces 400, large, tight tracking (-0.02em), sentence case: "One link does the whole job." Never bold for titles.
- Card titles in Inter 600 with a small line icon before them; body in Inter 400, grey-ink (#0C0C0C at 75%).
- Buttons are pills: ink with white text for the main one, white with ink text for the other.

## Motion
- Friendly and snappy. Bubbles pop in with `outBack` (scale 0.85 → 1, 0.35 s) after a 0.6 s typing indicator (three dots); reactions bounce on afterwards.
- The marker swipe draws left to right (stroke-dashoffset or clip-path, 0.5 s) just as the narrator says the phrase.
- Cards slide up 16 px and fade in, staggered by 0.12 s, sometimes resting at a slight tilt (−3° to 3°). A highlighted row in a list moves to the next row as the story moves on.
- Doodles draw themselves (stroke-dashoffset) and wiggle once.

## Structure and pacing
1. Hook: a two- or three-bubble chat over a photo that sets up the surprise ("wait, you made this??").
2. The big title with the marker on the promise.
3. Three or four feature cards in a grid, each showing the feature in a tiny app window.
4. "How it works" as a short chat between the viewer and a helper.
5. Close on the title again with one pill button-like call to action as text.
Beats last 3–5 s; keep the rhythm conversational.

## Narration voice
Warm, casual and a bit witty, like a friend showing you something neat; second person, contractions, short sentences, one light joke at most. Never salesy, never shouty.

## Articles
A friendly product-style explainer: a punchy opening line, then sections with `##` headings phrased as plain promises ("One link does the whole job"). Each section is two or three short paragraphs plus, where it helps, a small example conversation as a quote block or a short checklist. End with a brief "how to start" section.

## Captions
Bottom-middle, Inter 600 ink on a white pill with a hairline border; the current word gets the lime marker behind it.
