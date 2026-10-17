// What the landing page says and where it points. Claims stay within what
// the product does today (PRODUCT.md, PLAN.md); no invented numbers.

/** Where "Get early access" goes. Set ACCESS_URL when building. */
export const accessUrl: string = import.meta.env.ACCESS_URL ?? "#access"

export const title = "Ekin: content that keeps growing"

export const description =
  "Ekin turns your ideas, documents and search opportunities into YouTube videos, Shorts and articles on a schedule, and holds every piece for your review."

export interface Step {
  name: string
  verb: string
  text: string
}

export const steps: Step[] = [
  {
    name: "Plant",
    verb: "You set it up once",
    text: "A pipeline starts from a prompt, pages of your own documents, or the searches your site could rank for. Choose the language, the schedule and what it makes.",
  },
  {
    name: "Grow",
    verb: "The agent drafts",
    text: "On schedule it picks an idea, writes the script, records the narration and renders the video. Shorts, an article and its translations grow from the same story.",
  },
  {
    name: "Review",
    verb: "You decide",
    text: "Every draft waits on the review desk. Pin a note to a moment, scoped to script, voice, visuals, music or metadata, and the next revision takes it in. Or approve.",
  },
  {
    name: "Publish",
    verb: "It goes out",
    text: "Approved videos go to YouTube with chapters, captions and the thumbnail you picked. Articles go to your blog as a commit.",
  },
]

export interface Contrast {
  editor: string
  ekin: string
}

export const contrasts: Contrast[] = [
  {
    editor: "You build every timeline by hand.",
    ekin: "The agent drafts every piece; you judge it.",
  },
  {
    editor: "Output grows only with your hours.",
    ekin: "Output follows the schedule you set.",
  },
  {
    editor: "A change means opening the project again.",
    ekin: "A change is a note; the next revision applies it.",
  },
  {
    editor: "Shorts are a second edit of the same footage.",
    ekin: "Shorts are cut from the live video and laid out anew for vertical.",
  },
]

export interface Fact {
  title: string
  text: string
}

export const facts: Fact[] = [
  {
    title: "Turkish first, English too",
    text: "Scripts, narration and on-screen words are written for the language, not translated after the fact.",
  },
  {
    title: "Your server",
    text: "Ekin runs as one binary with SQLite on a small server you own. Rendering can go to a Mac or another worker.",
  },
  {
    title: "A team at the desk",
    text: "Owners, editors and viewers share a workspace. A daily email lists what waits for review.",
  },
]
