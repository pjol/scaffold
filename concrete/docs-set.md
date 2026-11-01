# Project docs

Every project carries the same root documents, so any agent can orient in minutes. The template ships them with the project name filled in; keep them current in the same change as the code.

| File | Purpose | Updated when |
|---|---|---|
| `README.md` | what the app is (3 lines), how to run it (`dev-up`), where the docs are | setup changes |
| `CLAUDE.md` | the agent guide: what this is, layout, commands, conventions, house rules, pointers into concrete/ and installed FEATURE.md files. Read in full before working. | any convention changes |
| `AGENTS.md` | one line pointing to CLAUDE.md (Codex reads AGENTS.md) | never |
| `ENV.md` | every variable: generated table (from `scaffold.json`) + hand-written notes on obtaining values | a variable is added or changes |
| `docs/ARCHITECTURE.md` | the product + technical spec and source of truth: domain model, roles, API surface, realtime topics, background jobs, decisions with reasons | before building a new domain area |
| `docs/DEPLOYMENT.md` | hosts, environments, secrets, build and release steps for backend, web, iOS, Android; store submission notes | infra or release changes |
| `docs/legal/` | Markdown exports of Terms and Privacy (generated) | legal versions bump |
| `design/README.md` | design rounds, the chosen option and palettes, how to regenerate shots | each design round |
| `branch-scopes/<branch>.md` | what shipped in a branch, why, and measured hours | before the branch merges |
| `scaffold.json` | name, slug, surfaces, installed features + versions, env spec | via the CLI |

## CLAUDE.md contents

Keep it under ~150 lines, in this order: what this is · read-first pointers (ARCHITECTURE.md, ENV.md, the concrete specs) · layout · commands · backend conventions · frontend conventions · house rules (no subtitles, tokens only, meaning-first copy, front pages only where needed) · installed features with their FEATURE.md paths · project-specific rules (things the user has asked for) · never-do list (never touch other running stacks, never OTA without a production build, never edit applied migrations, never commit secrets or user data).

## Branch scopes

Each branch records its work in `branch-scopes/<branch-name>.md` before merging: date range, repos touched, features large to small with hours each (nearest 0.1 h), a table of smaller fixes, totals, volume (files, insertions/deletions, migrations, new routes). Later sittings append `# Round N` rather than rewriting earlier numbers.

**Hours are measured wall-clock time, never an estimate of how long the work would take by hand.** Measure from session-transcript timestamps clustered into sittings (30-minute gap), using the `time-accounting` skill (`~/SKILLS/time-accounting`, public at github.com/pjol/SKILLS). State the method in one line and name its weakness. If the clock wasn't consulted, report volume and say time was not measured. Itemised hours must sum to the measured total.

## Writing docs

Follow writing.md. Docs are read by agents with no context: rules before reasons, tables for reference data, exact paths and commands, nothing that doesn't change what the reader does. Delete stale sections rather than annotating them.
