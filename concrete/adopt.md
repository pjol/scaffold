# Adopting an existing project

A project that wasn't made by `scaffold new` (its own backend or web, an older layout) joins the toolset through
`scaffold.json` and a `layout` block, so `add`, `env` and `doctor` work on it without rewriting what it already has.

## Steps

1. `scaffold adopt <dir> --name <Name> [--slug x] [--bundle-id com.x.app] --dry-run`, then without `--dry-run`. It
   detects the surfaces (`backend/`, `web/`, `mobile/`), marks the ones without the template's registries as
   `custom` (backend: `internal/api/registry.go` with `RegisterModule`; web and mobile: `src/features/registry.tsx`),
   finds the packages (`mobile/packages` when present), marks each custom surface's existing `.env.example` as
   hand-written, writes `scaffold.json`, adds `AGENTS.md` and `ENV.md` if missing, and never edits other files.
2. Declare every variable the code reads in `scaffold.json` → `env` (`scaffold env` lists the ones missing), with tiers
   and docs, then `scaffold env --write`. Hand-written examples (`layout.hand_env`) are left alone; doctor still
   checks they name every declared variable.
3. Add `<!-- scaffold:features -->` to CLAUDE.md's installed-features list.
4. Bring a template-shaped surface over from a reference project (`scaffold new <tmp> --name <Name> --slug <slug>
   --surfaces backend,web,mobile`): copy its `mobile/` and the packages it imports, keep the anchors
   (`feature-belt/SKILL.md` → Anchors), and adapt the shared API layer to the project's real API.
5. `scaffold doctor` until clean, or until each finding is a deviation written down in the project's docs.

## Layout

```json
"layout": {
  "packages": "mobile/packages",
  "custom": ["backend", "web"],
  "hand_env": ["backend", "web"]
}
```

| Key | Effect |
|---|---|
| `packages` | where `tokens`, `ui-native`, `ui-web`, `shared` live (default `packages`). Use `mobile/packages` when the web app predates the foundation and must keep installing on its own: `mobile/` is then its own pnpm workspace (`pnpm-workspace.yaml` with `packages/*`, hoisted linker) and no root workspace exists. `add` and `doctor` map every `packages/…` path here |
| `custom` | surfaces built outside the template. `add` writes nothing into them (no files, wiring, npm deps, env, `go mod tidy`) and prints a "Port by hand" list instead; `doctor` skips their migration ledger and house-rule scan |
| `hand_env` | tiers whose `.env.example` is the project's own; `env --write` skips them. Without `scripts/dev-up`, `env --write` writes no `.env.dev.example` beyond declared `dev` variables, and generated examples say "Copy to …" |

A feature added to an adopted project still records its version in `scaffold.json` and copies its FEATURE.md to
`docs/features/`; its backend and web code become the project's own code when ported, against the project's API
contract rather than the template's.
