# Agent instructions

Read `PLAN.md` for what Ekin is and `CONVENTIONS.md` before writing code.

## Commits

Commit as soon as a unit of work is complete (a feature, fix, refactor, or docs change that stands on its own). Do not wait to be asked. Do not batch unrelated work into one commit.

- Conventional Commits: `type: subject`
- Allowed types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `chore`, `ci`
- No scope (`feat(api):` is wrong; `feat:` is right)
- Single-line message only — no body, no footer, no bullet list
- No co-author trailers (`Co-Authored-By`, `Made-with:`, Cursor/Claude attribution)
- Imperative, lowercase subject after the type
- Never commit secrets, `.env` files, credentials, or generated binaries that are gitignored
- Never skip hooks (`--no-verify`) or amend pushed commits
- Work on the main branch, do not branch off.
- No Claude session links in commit messages

## Backend bugs

When a backend bug is found or fixed, add Go test case(s) that fail without the fix and pass with it. Prefer table-driven tests next to the code under test (`*_test.go`). Do not ship a backend fix without covering tests.
