# Ekin site

The public landing page: Astro (static), built with Bun, typechecked with tsgo,
linted with oxlint and formatted with oxfmt. Brand rules: `../brand/README.md`.

```sh
bun install
bun run dev            # http://localhost:4321
bun run build          # astro sync, tsgo, astro build → dist/
bun run lint
bun run format         # format:check in CI
```

Build-time settings:

| Env | Meaning |
|---|---|
| `SITE_URL` | Public origin, for canonical and social links. |
| `ACCESS_URL` | Where "Get early access" points (a form or a mailto). Defaults to the page's own access section. |

Copy lives in `src/site.ts` and the section components; claims stay within what the
product does today (PRODUCT.md). `dist/` is a plain static site for any static host.
