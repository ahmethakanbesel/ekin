---
name: Field engraving
description: Olive and sage brand boards, stippled engravings and mono spec labels; grounded, unhurried and built to last.
inspiration: Arc Studio's identity for Anything, an insurance brand for the people who build things
kinds: [video, article]
palette: ["#1E1E1C", "#5A5E4B", "#7E8670", "#DCE3DC", "#B9C1CB", "#C5C890", "#5DAF78", "#F2EFE7"]
headingFont: Inter
bodyFont: JetBrains Mono
musicMood: Low, steady felt piano over a soft analogue drone; patient, like early morning on a site.
---
## Look
- Every scene is a board from a brand book: a flat field of olive (#5A5E4B), sage (#DCE3DC) or ink (#1E1E1C) with a 6–8% inset margin, one subject placed with care, and lots of air.
- Engravings instead of photos: take a stock photo of hands, tools, buildings or landscapes, make it grayscale with high contrast, then stipple it with an SVG filter (feTurbulence at high frequency + feColorMatrix threshold) so it reads as a dotted print. Tint it with the field colour (mix-blend-mode: multiply on light fields, screen on dark ones). Classical objects (a column, a hand tool, a plant) work best as single cut-outs on one side of the frame.
- A soft gradient wash for title cards and the close: a blurred mesh of olive → lichen (#C5C890) → steel (#B9C1CB), like light on an overcast field. It is the only gradient.
- A small emblem drawn in SVG from the topic itself (strokes radiating from a centre, a seal, a monogram), never a real organisation's logo. Use it as a badge, a section marker or a repeating tile pattern on sage.
- Signal green (#5DAF78) appears at most once per scene, for the action or the answer.

## Typography
- Headlines in Inter 400–600, sentence case, tight tracking (-0.02em), set in two short lines: "Welcome to / your new policy."
- Lockups and names in Inter 800, all caps, wide tracking (0.08em).
- Every annotation in JetBrains Mono 500, all caps, 0.12em tracking, small, joined to what it names by a 1px hairline leader: "HORIZONTAL LOCKUP", "ST 2: 18 PX", "STEP 02".

## Motion
- Calm and architectural. Hairlines draw first (stroke-dashoffset, `outCubic`, 0.6 s), then the label types on letter by letter, then the subject fades up 12 px.
- Engravings reveal by their dots: animate the threshold so the image resolves from sparse speckle to full print over ~1 s.
- Between scenes the emblem turns 45° and the field colour wipes across behind it; no zooms faster than 3% per scene.
- Tile patterns drift diagonally at a walking pace.

## Structure and pacing
1. Open on a board with one plain, confident line and its mono label (first 2 s for shorts).
2. Lay out the subject like a specification: parts, numbered steps or variants, each on its own board with labelled leaders.
3. Show it in use: an engraving of the person or place it serves.
4. Close on the gradient wash with the emblem and one line to keep.
Boards last 4–6 s; long videos open each chapter with a mono "SECTION 01" card on ink.

## Narration voice
Grounded and respectful, like a good foreman explaining a plan: short declarative sentences, concrete nouns, no hype, no jokes at anyone's expense. Speaks to and for people who do real work.

## Articles
Structured like a field guide: a short plain-spoken introduction, then sections with `##` headings that name the part or step, each opening with a one-line summary in bold. Use numbered steps, small specification tables and "What to check" lists. Close with a short paragraph on why it matters to the people it serves.

## Captions
Bottom third, Inter 600 on no background, in bone (#F2EFE7) on dark boards and ink on light ones; the current word in signal green.
