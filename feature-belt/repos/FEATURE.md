# Repositories

## What it does

- A folder in the drive can be a git repository: "New repository" in a folder, or "Turn into repository" on one (its files become the first commit, its sharing moves over, the folder goes to the trash). Repositories share, move and trash like any drive item.
- Clone, fetch, pull and push over HTTPS (`<API>/git/<workspace>/<repo>.git`) with personal git credentials from Settings → Git credentials (read or read-write, expiry, last used, revoke). Agent credentials and sign-in tokens never work there.
- Browse in the app: tree with last commits, README, syntax-highlighted files, blame, history, commit diffs, branches with ahead/behind, tags, code search and a file finder (T). Edit, add, upload, rename and delete files in the browser; each save is one commit to the branch (when allowed) or to a new branch, and a branch that moved meanwhile is refused, not overwritten.
- Pull requests: compare and open, description, unified and split diffs with a file list, line comments in threads (resolve, outdated), reviews (comment, approve, request changes), review requests, merge commit or squash, branch protection (pull requests only, required approvals, stale approvals reset, no force-push or deletion), conflicts listed with local resolution steps, close and reopen, live updates.
- AI agents work as the person through tools: read the tree, files, history and pull requests; commit to a feature branch and open a pull request (with scope Suggest, the commit becomes a diff the person accepts). Merging is always a person's action.
- Mobile browses code, reads history and reviews pull requests (comments, line comments, approve); editing and merging are on the web.

## Data

| Table | Purpose |
|---|---|
| `repos_repos` | one row per repository; `id` is its drive item's id (`files_items`, cascade); URL slug unique per org, default branch, merge settings, next pull number, size, last push |
| `repos_protections` | branch rules by name or glob: pull requests only, required approvals, reset approvals on new commits, force-push, deletion |
| `repos_pulls` | pull requests: number per repo, head/base branch and commit, state, merge method and commit, who merged or closed |
| `repos_reviews`, `repos_comments`, `repos_review_requests`, `repos_pull_events` | reviews (the head reviewed, dismissed when stale), conversation and line comments (path, line, side, thread root, resolved), who's asked, the timeline |
| `repos_git_credentials` | sha256 of each credential, prefix, access, expiry, last used, revoked |

Git data: bare repositories at `REPOS_DIR/<id>.git`, plus `refs/pull/<n>/head` per pull request (kept after merge or branch deletion so its diff stays). In-app commits use `<user id>@users.noreply.<web host>` as the email.

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/repos/orgs/{orgID}/repos?q=&cursor=` | member | repositories you can open |
| POST | `/v1/repos` · `/v1/repos/import` | `repos.create` + edit on folder · + manage on folder, 60/h | create (`readme`) · turn a folder into one; `GET /v1/repos/folders/{id}/preview` plans it |
| GET / PATCH | `/v1/repos/{id}` | view / manage | details, clone URL, your level · description, default branch, slug, merge methods |
| GET / POST / PATCH / DELETE | `/v1/repos/{id}/protections[/{pid}]` | view / manage | branch rules |
| GET | `…/refs?counts=` `…/tree` `…/tree/commits` `…/blob` `…/raw` `…/download` `…/blame` `…/paths?q=` `…/search?q=` `…/commits[/{sha}]` `…/compare?base=&head=` | view | reading; `?ref=` (branch, tag, commit; default branch) and `?path=` |
| POST | `…/commits` | edit, 600/h | `{branch, expected_head, new_branch?, message, changes[put/delete/move/replace_lines]}`; 409 `head_moved`, 403 `branch_protected` |
| POST / DELETE | `…/branches` · `…/branches?name=` | edit, 60/h | create from a ref · delete (not default or protected) |
| GET / POST | `…/pulls?state=&q=&author=me&review_requested=me` | view / edit, 120/h | list · open `{title, body, head, base, reviewers}` |
| GET / PATCH | `…/pulls/{n}` | view / author or edit | detail with reviewers · title, body, base |
| POST | `…/pulls/{n}/close` `…/reopen` | author or edit | |
| GET | `…/pulls/{n}/timeline` `…/commits` `…/diff?path=&commit=` `…/threads` `…/mergeability` | view | |
| POST | `…/pulls/{n}/merge` | edit, a person (agents 403) | `{method, title?, message?, expected_head, delete_branch}`; 409 `not_mergeable` / `head_moved` / `base_moved` |
| POST / PATCH / DELETE / PUT | `…/pulls/{n}/comments[/{cid}[/resolved]]` | comment, 300/h | comment (line: `path, line, side`; reply: `reply_to`) · edit own · delete own (manage: any) · resolve |
| POST | `…/pulls/{n}/reviews` | comment; approve and request changes need edit, not the author | `{state, body, comments[]}` |
| POST / DELETE | `…/pulls/{n}/reviewers[/{uid}]` | author or edit | ask people who can view |
| GET | `…/people?q=` · `/v1/repos/git` | view · user | reviewer suggestions · git version and limits |
| GET / POST / DELETE | `/v1/repos/credentials[/{id}]` | user, people only, 30/h | your git credentials (secret shown once) |
| GET / POST | `/git/{org}/{repo}.git/{info/refs,git-upload-pack,git-receive-pack}` | Basic with a git credential | smart HTTP: fetch needs view, push needs edit + a read-write credential |

Permission model (`repos.repository`, an access resource under the repository's drive item, inheriting; agent scopes key on it):

| Level | May |
|---|---|
| view | clone/fetch, browse, read pull requests |
| comment | + comment, line comments, "comment" reviews, resolve threads they're in |
| edit | + push (non-protected branches), commit in the app, branches, open/merge/close pull requests, approve or request changes |
| manage | + settings, protection rules, delete anyone's comment; sharing through the shared dialog |

Org permission `repos.create` (owner, admin, member). Realtime `repos:<id>` (view): `repos.refs_changed`, `repos.pull_changed {number}`, `repos.repo_changed`. Notifications: `repos.review_requested`, `repos.pull_comment`, `repos.pull_review`, `repos.pull_merged`.

Agent tools (same checks; `repos.repository` targets, also from `Target({})`): reads `repos_list`, `repos_read_tree`, `repos_read_file`, `repos_search_code`, `repos_log`, `repos_diff`, `repos_list_pull_requests`, `repos_read_pull_request`; writes `repos_create_branch`, `repos_commit_files` (feature branch only, optional `open_pull_request`; `Preview` = every touched file before and after), `repos_open_pull_request`, `repos_comment_on_pull_request`. Search: repositories by name (`repos.repository`) and file paths in your 5 most recently changed repositories (`repos.file`).

## Screens

| Where | Web | Mobile |
|---|---|---|
| List | `/repos` (nav "Repositories") | `repos` (Home row) |
| Repository | `/repos/[id]` code · `blob` · `edit` (`new=1`) · `upload` · `search` · `commits[/sha]` · `branches` · `compare` (open a pull request) · `pulls[/n]` (Conversation, Commits, Files changed) · `settings`; one header with Clone and the ⋮ menu (`feature "repos", kind "repos.repository"`) | `repos/[id]` tree + README · `blob` · `commits[/sha]` · `pulls[/n]` (conversation, files, review sheet) |
| Create | `/repos/new?folder=` · `?from_folder=` (the drive's New menu and folder ⋮) | — |
| Settings | `/settings/git-credentials` | `settings/git-credentials` |

Every async view has skeleton, empty and error states; diffs collapse large files and fetch cut ones on demand.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `REPOS_DIR` | `.data/repos` (dev) | bare repositories; required outside development: a persistent volume |
| `REPOS_GIT_BIN` | `git` | git ≥ 2.38, checked at boot (`config.Problem` outside development) |
| `REPOS_MAX_PUSH_BYTES` | 104857600 | push cap (body and `receive.maxInputSize`); also the import cap |
| `REPOS_MAX_EDIT_BYTES` | 1048576 | largest file shown and edited in the browser |
| `REPOS_MAX_COMMIT_BYTES` | 26214400 | largest in-browser commit and download |
| `REPOS_GIT_TIMEOUT` / `REPOS_HTTP_TIMEOUT` | 30s / 30m | per git process / per clone or push |

npm: `highlight.js` (BSD-3) and `marked` (MIT) in web and mobile (the shared package imports them; the workspace is hoisted).

## Compliance

- Deletion: git credentials and review requests deleted; authorship removed from pull requests, reviews, comments and events (what was written stays); commits keep the author text recorded in git (history can't be rewritten; in-app commits never carry account emails). Repositories in a deleted person's My drive go when files purges it.
- Export: credentials (metadata), repositories created, pull requests, reviews and comments written.
- Pushed content belongs to the org; managers delete comments. No public links to repositories.

## Stitching

1. Hosting: mount `REPOS_DIR` on a persistent volume and route `/git/*` and repository writes to the one instance that holds it (see Extending → Scale). Install git 2.38+ in the image; pushes need a Unix host.
2. Decide the nav: keep "Repositories" in the main nav, or reach repositories only from the drive.
3. Choose who may create repositories (`repos.create`) and whether the default branch's rule should require approvals (the rule `newRepo` writes).

## Extending

- Seams: `files.RegisterItemType` (type `repo`, `CreatePath`, `ConvertPath`, `OnDelete`); the engine in `git.go`, `write.go`, `diff.go` (`ApplyChanges`, `MergeTrees`, `FastImport`, `DiffCommits`); `checkPush` for more push rules (signed commits, path rules).
- Scale: one writer. More API instances need a shared filesystem with working POSIX locks for `REPOS_DIR`, or a router sending `/git/*` and `/v1/repos/{id}` writes to the volume's instance. Ref updates are atomic (git lock files and `update-ref` old values); in-app writes also take a per-repository advisory lock.
- Time limits: `net/http/cgi` can't cancel its process, so a git HTTP request is cut on its next write past `REPOS_HTTP_TIMEOUT` (git sends keepalives every 5 s). Processes the API runs itself have hard deadlines.
- Out of scope: forks, SSH, Git LFS, tags created in the app, batched review comments, suggested-change blocks, CI status checks, code owners, rebase merging, images in READMEs (shown as links: an outside image tells its host who reads the page), editing on mobile.

## Provenance

New in the forum belt, built on `files` and `access`. Mechanics follow git's smart HTTP (`git http-backend`), plumbing-based server writes as hosted forges do them, and GitHub's pull request and protection model. The pre-receive check relays through a FIFO pair so the decision runs in Go with the database.

## Changes

- 1.0.0: first release (engine, smart HTTP with credentials and protection, in-app editing, pull requests and reviews, agent tools, search, web and mobile UI).
- 1.0.1: `highlight.js` and `marked` declared for `packages/shared` too (its repos helpers import them; hoisting only hid it).
- 1.0.2: no direct writes to access grants or owners: "Turn into repository" copies the folder's grants with `access.AddGrant` (a grant to a suspended account stays on the trashed folder) and its restricted flag with `access.SetInherits` (needs access 1.2.1), and the repository resource follows its drive item's owner through `access.SetOwner`; the `repos_commit_files` suggestion preview shows the pull request it would open (title and description) or the open one it adds to. Pull request notifications name their repository (`AgentTarget`, notifications ≥ 1.1.1), so an agent reading the inbox doesn't see them for a repository its scope hides.
- 1.0.3: Post-commit side effects run through `bg.Go` (template `internal/bg`), so shutdown and `cmd/seed` wait for them and a panic is logged instead of crashing the process: size recount (`repos.update_size`) and `git gc` after in-app writes (`repos.gc`). A pull request's “Check out locally” commands stay on one line and scroll sideways (a long branch name wrapped mid-word). gofmt. Search sets `OrgID` and `Context` (the org, then the repository for a file hit; template `app.SearchResult.Context`) and keeps to the org a search is scoped to (`app.SearchOrg`).
