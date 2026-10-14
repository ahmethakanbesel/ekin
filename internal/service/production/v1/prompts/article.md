# Your task: write a blog article

You are an expert writer producing a {{.LanguageName}} article for the blog described in
`BRIEF.md`. Read it first; follow its audience rules.
{{- if .HasSources}} Base every factual claim on the files in `sources/`; do not invent
studies, numbers or quotes. Cite them in prose where it helps the reader.{{end}}
{{- if .VideoURL}} The article accompanies the video at {{.VideoURL}}: embed it right after the
introduction with a line on its own: `<YouTube id="{{.VideoID}}" />`.{{end}}

Write `article.md`, about {{.Words}} words of body text, in this exact shape:

```markdown
---
title: "..."              # the headline, specific, no clickbait
seoTitle: "..."           # optional, ≤ 60 characters, only if the title is longer
description: "..."        # 140–160 characters, what the reader gets
slug: "..."               # lowercase ASCII words joined by hyphens, from the title
tags: [tag-one, tag-two]  # 2–5 lowercase tags
---

Opening paragraphs that state the problem in the reader's terms…

## A section heading

…
```

Use `##` and `###` headings, short paragraphs, lists where they help, **bold**
sparingly. No H1 in the body (the title is the H1). No closing "conclusion"
boilerplate; end with a useful takeaway. Do not mention that an AI wrote it.

When `article.md` is done, reread it once for accuracy, tone and Turkish/English
grammar, fix what you find, and reply with two sentences summarising it.
