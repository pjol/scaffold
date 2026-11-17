# Repositories

## What it does

- A folder in the drive can be a git repository: "New repository" in a folder, or "Turn into repository" on one (its files become the first commit, its sharing moves over, the folder goes to the trash). Repositories share, move and trash like any drive item.
- Clone, fetch, pull and push over HTTPS (`<API>/git/<workspace>/<repo>.git`) with personal git credentials from Settings → Git credentials (read or read-write, expiry, last used, revoke). Agent credentials and sign-in tokens never work there.
- Browse in the app: tree with last commits, README, syntax-highlighted files, blame, history, commit diffs, branches with ahead/behind, tags, code search and a file finder (T). Edit, add, upload, rename and delete files in the browser; each save is one commit to the branch (when allowed) or to a new branch, and a branch that moved meanwhile is refused, not overwritten.
- Issues: Markdown with @mentions (notified), labels (token tones), assignees, milestones with due dates and progress, close as completed or not planned, comments with reactions, pin (3) and lock, templates from `.github/ISSUE_TEMPLATE/*.md`, the search syntax (`is:open label:bug assignee:@me milestone:"v1" no:label sort:comments`), keyset pages. Issues and pull requests share numbers; `#12` anywhere links, and "Fixes #12" in a pull request (merged into the default branch) or a commit (reaching it) closes the issue.
- Pull requests: compare and open (from `pull_request_template.md`), drafts, labels, assignees, milestone, linked issues, unified/split diffs with "Viewed" checkboxes, hide whitespace and side-by-side image diffs, line and multi-line comments in threads, pending reviews (comments held until submit), ```` ```suggestion ```` blocks committed one by one or in a batch, reviews and re-requests, CODEOWNERS (owners asked automatically; "require review from code owners"), merge commit, squash or rebase, update branch, auto-merge, branch protection (pull requests only, approvals, stale approvals reset, code owners, required checks, force-push, deletion), conflicts with local steps, live updates.
- Commit statuses: CI posts state per context with a per-repository CI token; shown on commits and pull requests; required checks block merging.
- Releases on tags made in the app, Markdown notes (or written from merged pull requests), uploaded files; zip/tar.gz archives; line permalinks (`#L10-L20`); README images read from the repository (outside images stay links); insights (weekly commits, contributors). Outbound webhooks (push, pull requests, reviews, issues, comments, releases, statuses), HMAC-signed, with a delivery log and redeliver.
- AI agents work as the person through tools: read the tree, files, history, issues and pull requests; open, comment on and label issues; leave comment reviews with suggestions; commit to a feature branch and open a pull request (scope Suggest turns writes into suggestions). Merging, statuses, webhooks and settings are always a person's.
- Mobile browses code, reads history, lists, reads, opens and comments on issues, reviews pull requests (line comments, pending reviews, suggested changes shown) and lists releases; triage, editing and merging are on the web.
- Connected GitHub and GitLab (also self-managed) repositories open in the same screens, read and written live with the person's own account (see Connected platforms).

## Data

| Table | Purpose |
|---|---|
| `repos_repos` | one row per repository; `id` is its drive item's id (`files_items`, cascade); URL slug unique per org, default branch, merge settings, next pull number, size, last push |
| `repos_protections` | branch rules by name or glob: pull requests only, required approvals, reset approvals on new commits, force-push, deletion |
| `repos_pulls` | pull requests: number per repo, head/base branch and commit, state, merge method and commit, who merged or closed |
| `repos_reviews`, `repos_comments`, `repos_review_requests`, `repos_pull_events` | reviews (the head reviewed, dismissed when stale), conversation and line comments (path, line, side, thread root, resolved), who's asked, the timeline |
| `repos_issues`, `repos_issue_comments`, `repos_issue_events`, `repos_issue_labels`, `repos_issue_assignees` | issues (number shared with pulls, state + reason, milestone, locked, pinned), comments, timeline, labels, assignees |
| `repos_labels`, `repos_milestones`, `repos_pull_labels`, `repos_pull_assignees` | labels (name, token tone, description), milestones (due date, open/closed), their links to pull requests |
| `repos_reactions`, `repos_references` | emoji reactions on issues, pulls and comments; `#n` references (issue, pull or commit source; `closes`) |
| `repos_viewed_files` | per person "Viewed" marks with the blob they saw |
| `repos_ci_tokens`, `repos_statuses` | per-repository CI tokens (sha256); latest status per commit and context |
| `repos_releases`, `repos_release_assets` | releases on tags (draft, pre-release, notes); files under org-owned storage keys |
| `repos_webhooks`, `repos_webhook_deliveries` | hook URL, secret (signs, never returned), events; each delivery's body, status, response, redelivery |
| `repos_archive_tokens` | one-time archive download tokens (sha256, 2 minutes) |
| `repos_git_credentials` | sha256 of each credential, prefix, access, expiry, last used, revoked |
| `repos_links` | drive links (item type `repo_link`) to a connected repository: provider, web base, path, who made it; nothing from the platform |

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
| GET / POST · GET / PATCH / DELETE | `…/issues?q=&cursor=` · `…/issues/{n}` | view / comment, 120/h · view / author or edit / manage | search (counts, ignored qualifiers) · open `{title, body, labels, assignees, milestone_id, template}` · detail · title, body, state + reason (author or edit), milestone, lock, pin (edit) · delete (a person) |
| PUT · GET · POST / PATCH / DELETE | `…/issues/{n}/labels`·`/assignees` · `/timeline` · `/comments[/{cid}]` | edit · view · comment (locked: edit), own or manage | |
| PUT / DELETE · GET | `…/reactions` · `…/templates?kind=issue\|pull` | comment · view | `{subject_type, subject_id, emoji}` |
| GET / POST / PATCH / DELETE | `…/labels[/{id}]` · `…/milestones[/{id}]?state=` | view / edit | |
| POST · PUT · PUT / DELETE · POST | `…/pulls/{n}/ready`·`/draft` · `/labels`·`/assignees` · `/auto-merge` · `/update-branch` | author or edit · edit · edit, a person · edit | `{method}`; update merges the base into the head |
| POST · GET / PUT / DELETE · GET / DELETE | `…/pulls/{n}/suggestions` · `/viewed` · `/reviews/pending` | edit, 600/h · view · comment | `{comment_ids, message?, expected_head?}`; 409 `suggestion_outdated` |
| POST | `/v1/repos/{repoID}/statuses/{sha}` | public group; a CI token (`Bearer`), 600/min; agent, session and git credentials refused | `{state, context, description, target_url}` |
| GET · GET / POST / DELETE | `…/commit-states?shas=` · `…/ci-tokens[/{id}]` | view · manage, people only | combined state per commit · CI tokens (secret once) |
| GET / POST / PATCH / DELETE | `…/releases[/{id}]` · `/releases/notes?target=&previous=` · `/releases/{id}/assets[/{aid}]` | view (drafts: edit) / edit | tag made at `target` on publish · notes from merged pulls · `{key, name}` from `POST /v1/uploads` (`repos.release_asset`); download answers a 5-minute signed link |
| POST / DELETE · POST · GET | `…/tags` · `…/archive {ref, format}` · `…/insights` | edit · view, a person, 120/h · view | archive answers `{url}` to `GET /git-archive/{token}` (root, one use, 2 minutes, access checked again) |
| GET / POST / PATCH / DELETE / POST | `…/webhooks[/{id}[/ping]]` · `/deliveries[/{did}[/redeliver]]` | manage, people only | secret once; deliveries POST through `safehttp`, signed `X-Webhook-Signature-256` / `X-Hub-Signature-256: sha256=<hmac>` |
| GET / POST / DELETE | `/v1/repos/credentials[/{id}]` | user, people only, 30/h | your git credentials (secret shown once) |
| GET / POST | `/git/{org}/{repo}.git/{info/refs,git-upload-pack,git-receive-pack}` | Basic with a git credential | smart HTTP: fetch needs view, push needs edit + a read-write credential |

Permission model (`repos.repository`, an access resource under the repository's drive item, inheriting; agent scopes key on it):

| Level | May |
|---|---|
| view | clone/fetch, browse, read pull requests |
| comment | + comment, line comments, "comment" reviews, resolve threads they're in |
| edit | + push (non-protected branches), commit in the app, branches, open/merge/close pull requests, approve or request changes |
| manage | + settings, protection rules, delete anyone's comment; sharing through the shared dialog |

Issues: view reads, comment opens and comments, edit triages (labels, assignees, milestones, anyone's state, lock, pin), the author edits and closes their own, manage deletes.

Org permission `repos.create` (owner, admin, member). Realtime `repos:<id>` (view): `repos.refs_changed`, `repos.pull_changed {number}`, `repos.issue_changed {number}`, `repos.status_changed {sha}`, `repos.repo_changed`. Notifications (only to people who can view): `repos.review_requested`, `repos.pull_comment`, `repos.pull_review`, `repos.pull_merged`, `repos.issue_assigned`, `repos.issue_activity`, `repos.mentioned`.

Agent tools (same checks; `repos.repository` targets, also from `Target({})`): reads `repos_list`, `repos_read_tree`, `repos_read_file`, `repos_search_code`, `repos_log`, `repos_diff`, `repos_list_pull_requests`, `repos_read_pull_request`; `repos_list_issues` (search syntax), `repos_read_issue`; writes `repos_create_branch`, `repos_commit_files` (feature branch only, optional `open_pull_request`; `Preview` = every touched file before and after), `repos_open_pull_request`, `repos_comment_on_pull_request`, `repos_create_issue`, `repos_comment_on_issue`, `repos_label_issue` (names; before/after in Preview), `repos_review_pull_request` (comment reviews only, line comments with ```` ```suggestion ````). Search: repositories by name (`repos.repository`), issue titles (`repos.issue`) and file paths in your 5 most recently changed repositories (`repos.file`).

## Connected platforms

Needs a module implementing `app.Connections` (the connectors feature); without one nothing below appears. Capabilities (`app.CapabilityProvider`), asked for incrementally in Settings → Connections:

| Capability | Scopes | Gives |
|---|---|---|
| `repos.github.read` · `repos.gitlab.read` | `repo read:org` · `read_api read_repository read_user` | browse, history, compare, pull/merge requests, checks, search, import, links |
| `repos.github.write` · `repos.gitlab.write` | `repo read:org workflow` · `api write_repository` | comment, review, merge, edit files, branches, open pull/merge requests |

GitHub OAuth apps have no read-only scope for private repositories, so repos keeps the split itself: every write opens the connection with the write capability, and without it the level caps at view (`grant_write_url` offers it).

- **Seam.** A connected repository's id is `<provider>~<connection>~<path with / as ~>` (`github~c1~acme~web`, `gitlab~c2~acme~platform~api`). Every `{repoID}` route picks the implementation from the id (`viaRead`/`viaWrite`): the git engine for UUIDs, a `RepoSource` (`source.go`; `github.go` maps REST v3, `gitlab.go` REST v4) for connected ones, answering the same JSON plus `source`, `web_url`, `supports` (what the host offers: no blame on GitHub REST, no "request changes" on GitLab REST, CI checks on both). API bases come from the connection's `api`/`web` endpoints, so GitHub Enterprise and self-managed GitLab (also under a path) work.
- **Levels.** GitHub `admin`→manage, `push`/`maintain`→edit, `triage`/`pull`→comment; GitLab Maintainer+→manage, Developer→edit, Reporter→comment, Guest→view.
- **Writes.** Edits commit through the contents API (one file; the file's blob as of the edit's base commit is the conflict check) or the git data API (several files; a fast-forward-only ref update from the base) on GitHub, and the commits API with each file's `last_commit_id` on GitLab: a file changed since answers 409 `head_moved`. Merge uses the methods the repository allows; protection and required checks answer 409 `not_mergeable` with the platform's reason, a moved head 409 `head_moved`.
- **Errors.** The platform's 401 → 409 `connection_needs_auth` with `fix_url` (never a sign-out here), rate limits → 429 `connection_rate_limited`, other refusals keep the platform's message.
- **Routes.** `GET /v1/repos/sources?org_id=` (`FeatureSources`), `GET /v1/repos/connections/{id}/repos?q=&sort=recent|name&owner=&cursor=` (+ `owners`), `GET …/commits/{sha}/checks` (CI checks; "none" for the app's own), `POST /v1/repos/connected/import` (`repos.create`; git over HTTPS through the connection's client via a loopback relay, branches and tags, capped at `REPOS_MAX_PUSH_BYTES`), `POST /v1/repos/links` · `GET /v1/repos/links/{id}` (a drive link; whoever opens it is checked live with their own connection to that host).
- **Agents.** The read tools take connected `repo_id`s and return `source`; `repos_list` takes `source` (`app`, `connected`, `github`, `gitlab` or a connection id). Writes are `repos_github_*` / `repos_gitlab_*`: `create_branch`, `commit_files` (feature branches only, optional pull/merge request), `open_pull_request`, `comment_on_pull_request`; `Source` "GitHub"/"GitLab", target `repos.external` `<connection>:<owner/repo>`, exact Preview, the connection's scope (`connectors:<id>`) checked at suggest in Preview and at edit in Handler. No merge tool. Search adds connected repositories (`repos.repository`) and code (`repos.file`) in each account's 3 most recently active repositories, 1.5 s per connection, `Context` "GitHub · account", `source` set.
- **Caching.** Answers keyed by commit id for 10 minutes, everything else for seconds, per person and connection, in memory; platform content is never stored.
- Out of scope: GitHub blame and thread resolution (GraphQL), GitLab "request changes", reviewer requests, protection settings (managed on the platform; "Manage access in GitHub/GitLab" in ⋮), a push mirror from the app's repositories (one person's token pushing on everyone's behalf; see Extending), webhooks (screens poll).

## Screens

| Where | Web | Mobile |
|---|---|---|
| List | `/repos` (nav "Repositories"): the workspace's own, then a Connected section and source chips (`?source=`: All shows 5 of each account, one account shows its repositories with search, recent activity or name, owner) | `repos` (Home row) with the Connected section · `repos/connected/[id]` |
| Repository | `/repos/[id]` code · `blob` (line links) · `edit` · `upload` · `search` · `issues[/n]` · `issues/new` · `labels` · `milestones` · `pulls[/n]` (Conversation, Commits, Files changed) · `commits[/sha]` (statuses) · `branches` · `compare` · `releases` · `insights` · `settings` (merging, protection, CI tokens, webhooks); one header with Clone (+ zip/tar.gz) and the ⋮ menu (`feature "repos", kind "repos.repository"`) | `repos/[id]` tree + README · `blob` · `commits[/sha]` · `issues[/n]` · `issues/new` · `releases` · `pulls[/n]` (conversation, files, review sheet) |
| Create | `/repos/new?folder=` · `?from_folder=` (the drive's New menu and folder ⋮) | — |
| Settings | `/settings/git-credentials` | `settings/git-credentials` |
| Links | `/repos/links/[id]` opens the linked repository with your own connection, or says how to connect | — |

Connected repositories use the same screens (no Issues, Releases or Insights tabs) with a `SourceBadge` in the header, rows and pull requests; ⋮ adds Open in GitHub/GitLab, Manage access there, Import into Scaffold App, Add to a Scaffold App folder; Settings, git credentials, reviewer requests and unsupported actions are hidden; checks show in the merge box and on commits.

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

- Deletion: git credentials, review requests, assignments, reactions, viewed marks, unsubmitted reviews and archive tokens deleted; authorship removed from issues, pull requests, reviews, comments, events, releases, milestones, CI tokens and webhooks (what was written stays); commits keep the author text recorded in git (history can't be rewritten; in-app commits never carry account emails). Release files are keyed by the org and outlive the uploader. Repositories in a deleted person's My drive go when files purges it.
- Export: credentials (metadata), repositories created, issues, issue comments, pull requests, reviews, comments and releases written.
- Pushed content belongs to the org; managers delete comments and issues. No public links to repositories. Webhook secrets sit in the database (they sign each delivery) and are never returned after creation; deliveries keep their body 30 days (200 per hook).

## Stitching

1. Hosting: mount `REPOS_DIR` on a persistent volume and route `/git/*` and repository writes to the one instance that holds it (see Extending → Scale). Install git 2.38+ in the image; pushes need a Unix host.
2. Decide the nav: keep "Repositories" in the main nav, or reach repositories only from the drive.
3. Choose who may create repositories (`repos.create`) and whether the default branch's rule should require approvals (the rule `newRepo` writes).
4. Webhook URLs and CI need egress and ingress: deliveries leave through `safehttp` (public https only outside development); CI reaches `POST /v1/repos/{id}/statuses/{sha}` on `PUBLIC_API_URL`. Release files use the storage bucket (purpose `repos.release_asset`, capped at `REPOS_MAX_PUSH_BYTES`).
5. Read seam for link cards: `repos.IssueRef(ctx, q, viewer *auth.Identity, repoID, number) (*repos.LinkedIssue, error)` reads #number of one of the app's repositories (a pull request or an issue: they share numbers) with the check of the pull request and issue routes (`access_level` on `repos.repository`, capped by `access.ViaAgent(ctx)`, trashed repositories refused), records nothing, and returns `Kind` (`pull`/`issue`), title, state (pull: open, merged, closed; issue: open, closed, with `StateReason`), `Draft`, repository name, `Context` (org · repository), canonical `Path`, level and `UpdatedAt`; else `repos.ErrIssueNotFound`. `repos.IssueFromPath(path)` reads `/repos/<id>/pulls/<n>` and `/repos/<id>/issues/<n>` (connected repositories answer false). Use it for `channels.RegisterUnfurler("/repos/", …)`, returning nil on not found.

## Extending

- Another host (Bitbucket, Gitea): implement `RepoSource` and `connectedLister`, add the provider to `platformNames` and `openRemote`, declare its capabilities.
- Push mirror: a repository setting that pushes each accepted push to a connected remote would run with one person's token for everyone; build it with a dedicated machine account on the connection when needed.
- Seams: `files.RegisterItemType` (type `repo`, `CreatePath`, `ConvertPath`, `OnDelete`); the engine in `git.go`, `write.go`, `diff.go` (`ApplyChanges`, `MergeTrees`, `FastImport`, `DiffCommits`); `checkPush` for more push rules (signed commits, path rules).
- Scale: one writer. More API instances need a shared filesystem with working POSIX locks for `REPOS_DIR`, or a router sending `/git/*` and `/v1/repos/{id}` writes to the volume's instance. Ref updates are atomic (git lock files and `update-ref` old values); in-app writes also take a per-repository advisory lock.
- Time limits: `net/http/cgi` can't cancel its process, so a git HTTP request is cut on its next write past `REPOS_HTTP_TIMEOUT` (git sends keepalives every 5 s). Processes the API runs itself have hard deadlines.
- Rebase merges replay each commit as base merged with it (git 2.38 `merge-tree` has no `--merge-base`); a branch with merge commits or a conflicting step can't rebase. CODEOWNERS (`.github/`, root, `docs/`) is read from the base; owners are workspace emails or `@team` / `@org/team` (access team handles); the last matching rule wins.
- Out of scope: forks, SSH, Git LFS, check runs with annotations (statuses only), issue forms (YAML templates), project boards, issue transfer, expanding diff context in place, multi-line selection in the web diff (agents and the API comment on ranges), connected GitHub/GitLab issues, outside images in Markdown (shown as links: an outside image tells its host who reads the page), triage and editing on mobile.

## Provenance

New in the forum belt, built on `files` and `access`. Mechanics follow git's smart HTTP (`git http-backend`), plumbing-based server writes as hosted forges do them, and GitHub's pull request and protection model. The pre-receive check relays through a FIFO pair so the decision runs in Go with the database.

## Changes

- 1.0.0: first release (engine, smart HTTP with credentials and protection, in-app editing, pull requests and reviews, agent tools, search, web and mobile UI).
- 1.0.1: `highlight.js` and `marked` declared for `packages/shared` too (its repos helpers import them; hoisting only hid it).
- 1.0.2: no direct writes to access grants or owners: "Turn into repository" copies the folder's grants with `access.AddGrant` (a grant to a suspended account stays on the trashed folder) and its restricted flag with `access.SetInherits` (needs access 1.2.1), and the repository resource follows its drive item's owner through `access.SetOwner`; the `repos_commit_files` suggestion preview shows the pull request it would open (title and description) or the open one it adds to. Pull request notifications name their repository (`AgentTarget`, notifications ≥ 1.1.1), so an agent reading the inbox doesn't see them for a repository its scope hides.
- 1.0.3: Post-commit side effects run through `bg.Go` (template `internal/bg`), so shutdown and `cmd/seed` wait for them and a panic is logged instead of crashing the process: size recount (`repos.update_size`) and `git gc` after in-app writes (`repos.gc`). A pull request's “Check out locally” commands stay on one line and scroll sideways (a long branch name wrapped mid-word). gofmt. Search sets `OrgID` and `Context` (the org, then the repository for a file hit; template `app.SearchResult.Context`) and keeps to the org a search is scoped to (`app.SearchOrg`).
- 1.1.0: GitHub and GitLab (incl. self-managed) repositories through `app.Connections`: capabilities `repos.{github,gitlab}.{read,write}`, a `RepoSource` seam behind every `{repoID}` route, browsing, history, compare, pull/merge requests with line comments, reviews, merges, CI checks, in-browser commits with conflict checks, branches; `GET /v1/repos/sources`, connected lists, import into the app, drive links (`repo_link`, migration 2401), connected search, agent read tools on connected ids and `repos_github_*`/`repos_gitlab_*` write tools; web Connected section, source chips and badges; mobile connected lists and badges. Needs agents ≥ 1.1.1 (ids with `/`).
- 1.2.0: Issues (labels, assignees, milestones, close reasons, reactions, mentions, `#n` references, closing keywords from pull requests and commits, templates, pin, lock, search syntax), pull request depth (drafts, labels/assignees/milestones, linked issues, pending reviews, multi-line and suggestion comments applied singly or batched, re-request, files viewed, hide whitespace, image diffs, CODEOWNERS with required owner review, rebase merge, update branch, auto-merge, PR templates), commit statuses with per-repository CI tokens and required checks, releases with tags and files, zip/tar.gz archives, line permalinks, README images from the repository, insights, outbound HMAC-signed webhooks with a delivery log, issue agent tools and comment reviews, issue search; web Issues/Labels/Milestones/Releases/Insights screens and settings (CI tokens, webhooks); mobile issues and releases (migration 2402).
- 1.2.1: read seam for link cards (`IssueRef`, `IssueFromPath`, `ErrIssueNotFound`).
