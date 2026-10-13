import { en, type Messages } from "./en"

// One locale for now. Adding Turkish means a `tr: Messages` catalogue and
// choosing between them here (or in a context, if it must switch live).
export const m: Messages = en

export type { Messages }
