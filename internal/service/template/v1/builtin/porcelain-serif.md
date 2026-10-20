---
name: Porcelain serif
description: Fine serif titles on porcelain white, soft blue light, frosted notifications and lines that come into focus as they are spoken.
inspiration: Calm serif product sites for personal assistants
kinds: [video, article]
palette: ["#FAFAF8", "#0E1630", "#F2F4F7", "#8A8F98", "#3E5BD8", "#9DB6F2", "#E7ECF7", "#141B2E"]
headingFont: Instrument Serif
bodyFont: Inter
musicMood: Airy, slow electric piano and soft sub warmth; unhurried, expensive, like a quiet hotel lobby at night.
---
## Look
- Porcelain white (#FAFAF8) or cool white (#F2F4F7) fields, very few objects, generous margins. Ink is a deep navy (#0E1630), never pure black.
- Light, not graphics: large blurred radial glows in blue (#3E5BD8) and periwinkle (#9DB6F2), filter: blur(60–100px), rising from the bottom or behind the subject like light through frosted glass.
- Frosted cards: notifications, messages and small panels on white at 80% opacity with backdrop-filter blur, a 1px white border, a soft long shadow and 18–22 px radius. They hold the evidence: a reminder, a quote, a figure.
- One still life per video: a stock photo of a single quiet object (a flower, a cup, an old telephone, a book) cut out on white, slightly off-centre, between two halves of a title.
- The finale switches to night: a deep navy field (#141B2E) with the blue glow behind one frosted card and one short line.

## Typography
- Titles in Instrument Serif 400, very large, tight leading (1.0–1.05), sentence case: "Wake up to a day, not a list." Use the italic for the one word that turns the sentence.
- Supporting text in Inter 400, small, navy. Feature numbers in Instrument Serif: "01", "02".
- Read-along paragraphs: show three or four short lines of the narration in Instrument Serif; the line being spoken is ink, the others sit at grey (#8A8F98, 60%).

## Motion
- Soft focus everywhere: elements enter with blur 12 px → 0 and opacity 0 → 1 over 0.7 s (`outCubic`), never a hard cut.
- Notifications slide down from the top edge with a small settle (`outBack`, low overshoot) and stack; older ones dim and shrink 4%.
- Glows breathe slowly (scale ±4% over 6 s) and drift; the still life has a 2% Ken Burns.
- A title can split around the still life: the left half slides in from the left, the right half from the right.

## Structure and pacing
1. A promise in one contrast line ("not X, Y") over the glow (first 2 s for shorts).
2. The everyday problem in a read-along paragraph.
3. Two or three numbered features, each a title, one line and a frosted card that shows it.
4. A quiet testimonial-like line or reflection, centred in serif.
5. Night finale: one card, one line.
Scenes breathe: 5–8 s each.

## Narration voice
Calm, assured, personal: speaks to "you", promises little and precisely, favours short contrast pairs and concrete moments (a morning, an inbox, a calendar). No exclamation marks.

## Articles
Editorial and spacious: an opening of two or three short paragraphs that each end on a clean line, then sections with `##` headings written as short serif-worthy sentences. Numbered sections ("01 …") for practical parts, one pull quote, no long bullet lists. End with a single reflective paragraph.

## Captions
Bottom-middle, Instrument Serif 400, large and navy on light scenes or white on night scenes; the current word at full strength, the rest at 55% opacity.
