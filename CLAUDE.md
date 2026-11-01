# CLAUDE.md — maintaining scaffold

This repo is the skill tree and toolset future agents use to build apps. Its readers are agents with no context who must read and follow every spec, so the specs stay short, exact and current.

## Layout

- `SKILL.md`, `concrete/SKILL.md`, `feature-belt/SKILL.md` — skill entry points (frontmatter `name`, `description`; validate with `python3 ~/.codex/skills/.system/skill-creator/scripts/quick_validate.py <dir>`).
- `concrete/*.md` — foundation rules. `template/` — the same foundation as a running app.
- `feature-belt/<name>/` — `feature.json`, `FEATURE.md`, code by surface (contract in `feature-belt/SKILL.md`).
- `cli/` — Go module `github.com/pjol/scaffold/cli`. `core/` holds every operation; `adapters/` (cliargs, mcpserver, httpapi) are thin and generic. Add behaviour as a new operation in `core` (`define(name, summary, fn)` in `ops.go`); never special-case an op in an adapter.

## Rules

1. **Template and specs move together.** A foundation change edits `template/` and the matching `concrete/*.md` in the same change.
2. **The template is real code.** It must build and run as-is with the placeholder literals (`scaffoldapp`, `Scaffold App`, `SCAFFOLDAPP`, `com.example.scaffoldapp`); the CLI replaces them. Never put `{{templating}}` syntax in code.
3. **Anchors are API.** A comment that opens with `scaffold:<name>` (`// scaffold:nav`, `{/* scaffold:tabs */}`, `<!-- scaffold:features -->`) is where `scaffold add` inserts lines; renaming or removing one breaks features (doctor reports it). Mentions in prose or doc comments are ignored.
4. **Features are versioned.** Bump `version` in `feature.json` on every change and note it at the bottom of FEATURE.md.
5. **Verify by generating.** After any change: `bin/scaffold new <scratch>/x --name X --features <all>` → `go build ./... && go vet ./...` in `backend/`, `pnpm install && pnpm typecheck && pnpm check` at the root, `scripts/dev-up/dev-up.sh` boots, `bin/scaffold doctor --dir <scratch>/x` is clean. Delete the scratch project after.
6. **Specs stay lean.** Each concrete file is the shortest text that fully specifies its area. Prefer pointing at template code over restating it. Follow `concrete/writing.md`.
7. **Don't vendor licensed third-party code** (OriginKit components, fonts with restrictive licences) into `template/` or the belt.
8. **No secrets, no real user data**, anywhere in this repo.

## CLI

```sh
cd cli && go build ./... && go vet ./...
bin/scaffold help
```
