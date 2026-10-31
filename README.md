# scaffold

The app-building skill tree and toolset for the house stack: Go backend (chi, pgxpool, Postgres, S3), Next.js web, Expo mobile, Clerk auth.

```
SKILL.md          root skill: what to read, how to start an app or add a feature
concrete/         the foundation as rules (stack, backend, database, auth, web, mobile, design,
                  design process, writing, env, dev-up, docs, compliance)
template/         the foundation as a running app — what `scaffold new` copies
feature-belt/     feature modules: notifications, moderation, orgs, support, identity, mcp,
                  chat, feed, places, events, payments, wallets, assistant
cli/              the scaffold toolset (Go): one operation registry → CLI, MCP server, HTTP API
bin/scaffold      runs the CLI from source
```

## Use

```sh
bin/scaffold list                                   # what features exist
bin/scaffold new ~/code/myapp --name "My App" --features chat,feed
cd ~/code/myapp && scripts/dev-up/dev-up.sh         # whole stack, zero keys needed
bin/scaffold add payments --dir ~/code/myapp        # later: add a feature, then stitch it
bin/scaffold doctor --dir ~/code/myapp              # check a project against the foundation
```

Agents: load `SKILL.md`. As an MCP server: `claude mcp add scaffold -- /abs/path/bin/scaffold mcp`. Over HTTP: `bin/scaffold serve` then `GET http://127.0.0.1:7420/v1/ops`.

## Install as skills

The three `SKILL.md` files (root, `concrete/`, `feature-belt/`) are valid skills (each with `agents/openai.yaml` for Codex). This repo is `github.com/pjol/scaffold` (private), cloned at `~/scaffold`; projects built from it refer to that path. `github.com/pjol/SKILLS` lists it through a pointer entry (`scaffold/`), which replaced `backend-setup` and `user-facing-app-context`. To load it as an installed skill, symlink the directory into a skills folder: `ln -s ~/scaffold ~/.claude/skills/scaffold`.

## Requirements

Go 1.26+, Node 22 + pnpm 10, Postgres 17 binaries (or Docker), Xcode/Android SDK for mobile builds.
