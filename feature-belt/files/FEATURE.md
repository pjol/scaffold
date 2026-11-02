# Files

## What it does

- An org drive: My drive (private until shared), Shared folders (org-owned, for a team or everyone), Shared with me, Recent, Starred and Trash (restore; purged after `FILES_TRASH_DAYS`). Nested folders; names are unique per folder.
- Upload by drag and drop (files and whole folders), pickers or the photo library; progress, cancel and retry; new versions with history (download or restore any). Rename, move (picker or drag), copy, star, trash, zip download of a selection.
- Previews: images, PDF, video, audio, text, code, Markdown, CSV tables and SVG (drawn as an image); office and other files download. Every download is a short-lived presigned link with a forced type; anything that can run script (HTML, SVG, XML, unknown) comes back as an attachment, and an upload whose bytes don't match an inline type (HTML named `.png`) is refused.
- Sharing through access: people, teams, the whole org, and public links (`/f/<token>`, optional expiry, revocable; needs `access.share_public`). Public pages preview and download a file or browse a folder; downloads are counted and limited per IP; visitors can report; staff turn public access off.
- Org quota and per-file limit shown in the UI; name and text search; activity per item (uploads, versions, renames, moves, sharing); AI agents read and make single, reviewable edits as the person.
- Connected drives (with the `connectors` feature): Google Drive (My Drive, Shared with me, shared drives), OneDrive and SharePoint libraries show under "Connected", browsed, previewed and changed live with each person's own account; Copy into Files, shortcuts colleagues open with their own connection, Save to Google Drive / OneDrive.

## Data

| Table | Purpose |
|---|---|
| `files_items` | folders, files and registered item types: org, parent, `drive` my/shared, owner (the My drive's person; null for org content), current version copied for lists and search (type, size, `text_like`, first 100 KB of text), trash (`trash_root`, `purge_after`), staff public-disable |
| `files_versions` | every version: object key (`files/<org id>/…`), size, type, uploader, note; the newest 100 kept |
| `files_uploads` | presigned uploads in flight (what each key was issued for) |
| `files_stars`, `files_recents` | per person |
| `files_activity` | events per item; `folder_id` lets a folder show its children's |
| `files_public_stats` | public previews and downloads per item per day |
| `files_archives` | 5-minute zip tokens (sha256 stored) |
| `files_object_deletions` | objects to delete, filled by triggers on version and unfinished upload rows (purge, org deletion, pruning) |
| `files_links` | a shortcut item's target on a connected platform: provider, product, the platform's id, name/kind/web address the pinner chose (never content or tokens) |
| `files_connected_downloads` | 15-minute links that stream a connected file through the API (token sha256, person, connection, ref, type, disposition) |

Every item is an access resource of kind `files.item` with its folder as parent.

## Permissions

| Who | Can |
|---|---|
| View (inherited from the folder, or granted) | open, preview, download, zip, copy, star, see versions and activity |
| Edit | upload into a folder, create, rename, move within the same drive, new or restored versions, trash and restore |
| Manage | share, public links (also `access.share_public`), restrict from the folder, move to another drive (hands the subtree to it), delete forever, empty trash |
| Owner | a My drive's person manages everything in it, whoever uploaded; shared folders have no owner: their creator gets manage and org owners and admins manage everything in them, restricted subfolders included |
| Org permission `files.create_shared_folders` | create top-level shared folders (default owner, admin) |
| Staff (support, admin) | list public items, turn public access off and on (audited) |
| AI agent of a person | the person's level capped by their agent scope and at edit; no delete, trash, sharing, or moves that change who can open an item (another drive, or a folder whose inherited sharing differs; also never suggested) |

Comment is view here (files have no comments). Org admins don't see a member's private My drive items.

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/files/types` | user | registered item types |
| GET | `/v1/files/orgs/{orgID}/drive` · `/items?section=&sort=&dir=&thumbs=1` · `/search?q=` | member | quota and limits · a section (keyset) · name and text search |
| POST | `/v1/files/orgs/{orgID}/folders` · `/files` | member, active | `{parent_id?, name, shared?, general?}` · text file `{name, text}` (empty files too) |
| POST · DELETE | `/v1/files/orgs/{orgID}/uploads` · `/v1/files/uploads/{id}/complete` · `/v1/files/uploads/{id}` | edit on the folder or file | presign `{parent_id?, item_id?, name, content_type, size}` · complete `{on_conflict}` (checks size and type) · cancel |
| POST | `/v1/files/orgs/{orgID}/archives` · DELETE `…/trash` | member | zip link `{ids}` · empty trash (manage) |
| POST | `/v1/files/batch` | per item | `{action: move\|trash\|restore\|delete\|star\|unstar, ids, parent_id?}` → per-item results |
| GET | `/v1/files/items/{id}` · `/children` · `/url?disposition=&version=` · `/text` · `/versions` · `/activity` | view | item with path · folder listing · download link · text (1 MB) · history · activity |
| PATCH · POST | `/v1/files/items/{id}` · `/move` · `/copy` · `/trash` · `/restore` · `/versions/{vid}/restore` | edit (copy: view + edit on target) | rename · move (manage across drives) · copy · trash · restore · restore version |
| PUT · DELETE | `/v1/files/items/{id}/star` · DELETE `/v1/files/items/{id}` | view · manage | star · delete a trashed item for good |
| GET | `/v1/files/public/{token}` · `/items/{id}` · `/children` · `/url` · `/text` | public, per-IP limits | what a link opens (no account; refuses agent credentials) |
| GET | `/files-archive/{token}` | zip token | streams the zip, re-checking access per item |
| GET · POST | `/v1/admin/files/public?status=` · `/items/{id}` · `/disable` · `/enable` | support, audited | public items with traffic; turn public access off and on |

Connected drives (`connected_*.go`; every route as the caller through `app.Connections`, `?org_id=` applies that workspace's policy):

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/files/sources` | user | `FeatureSources`: the app's drive, each connected account (status, `can_write`, `write_url`), offers |
| GET | `/v1/files/connected/{connID}/drives` · `/search?q=` | read capability | drives (platform cursor) · the platform's search |
| GET | `/v1/files/connected/{connID}/items/{ref}` · `/children?sort=&dir=&thumbs=1&cursor=` · `/url?disposition=` · `/text` · `/sharing` | read | item with path · folder page (platform cursor) · 15-minute proxied link · first 1 MB of text · platform sharing summary |
| POST · PATCH | `…/folders` · `…/items/{ref}` · `…/items/{ref}/move` · `…/items/{ref}/trash` · `…/uploads` | write capability | new folder · rename · move within the drive · platform trash · resumable session the browser PUTs chunks to |
| POST | `…/items/{ref}/copy-to-files` `{org_id, parent_id?}` | read + edit on the folder | Copy into Files (stored through the drive's storage path, quota, versions) |
| POST · GET | `/v1/files/orgs/{orgID}/shortcuts` · `/v1/files/items/{id}/shortcut` | member + edit · view | pin a connected item as a `link` item · resolve it with the viewer's own connection |
| POST | `/v1/files/items/{id}/save-to` `{connection_id, parent_ref?}` | view + write capability | Save to Google Drive / OneDrive |
| GET | `/files-connected/{token}` | link token | streams the file (Range passed through), forced type and disposition |

Errors: 404 without view, 403 `access_level_required` / `agent_forbidden` (an agent's move that would change who can open the item), 409 `name_taken` / `item_trashed` / `restore_parent` / `target_changed`, 422 `content_type_mismatch` / `size_mismatch`, 507 `quota_exceeded`. Realtime: `files:<folder id>` (and the org id for shared folders, `user:` for My drive) carry `files.changed`; lost access revokes them. Notification `files.public_disabled`. Moderation target `files.item` (remove = turn public access off). Search: `files.item` (`Context` = org · folder, or My drive / Shared folders / Shared with me; scoped by `app.SearchOrg`). Agent tools: `files_search`, `files_list_folder`, `files_get_item`, `files_read_text` (read); `files_create_text_file`, `files_replace_lines`, `files_rename`, `files_move` (Target + exact Preview; edits refuse a stale `base_version_id`).

## Screens

| Where | Web | Mobile |
|---|---|---|
| Drive | `/files`, `/files/shared`, `/shared-with-me`, `/recent`, `/starred`, `/trash`: side sections and storage meter (title menu on phones), New (folder, upload files or a folder, shared folder, registered types), search, sort, list or grid, multi-select bar (download, move, star, trash; restore and delete in Trash), drop zone, keyboard (arrows, Space, Enter, ⌘A, Esc, Delete, F2, /), drag rows onto folders or crumbs, upload tray | Home → Files: section sheet, search, Add (photos, files, folder), rows with ⋮ |
| Item | `/files/[itemID]`: folder listing with crumbs, or file preview with Details · Versions · Activity; ⋮ `ResourceMenu` everywhere (Share… and AI agent access from other features); shortcuts open their target or say Connect / Ask for access | `files/[itemID]`: folder or preview, Open in browser, Save or send, versions; ⋮ in the header; shortcuts likewise |
| Connected | Sidebar "Connected" (`ConnectedSection`, the open account's drives under it; `ConnectPrompt` when nothing is connected, dismissible); `/files/c/[connectionID]` drives; `/files/c/[connectionID]/[ref]` folder (same rows, grid, sort, crumbs, multi-select, keyboard, drag-move, drop-upload) or preview + details with the platform's sharing; `SourceBadge` on every row; Files search gets `SourceFilter` chips | Files home "Connected" section; `files/c/[connectionID]` drives, `files/c/[connectionID]/[ref]` browse, search, preview (image, text, Markdown, CSV; others in the browser), Open in app, Save or send, Copy into Files, Add shortcut; badges |
| Public | `/f/[token]`: preview and Download, folder browsing, Report (signed in) | web only |
| Admin | `/admin/files`: public now / turned off, traffic, turn off with a reason | none |

Skeleton, empty and error states on every view; cached lists stay on screen with a "Couldn't refresh" note when offline.

## Config

| Variable | Default | Doc |
|---|---|---|
| `FILES_ORG_QUOTA_GB` | 100 | per org, every version and the trash; 0 = none |
| `FILES_MAX_UPLOAD_MB` | 250 | largest upload |
| `FILES_TRASH_DAYS` | 30 | before trash is purged |
| `FILES_DELETED_ACCOUNT_KEEP_DAYS` | 30 | a deleted account's private items stay restorable by org admins; 0 = deleted with the account |
| `FILES_PUBLIC_DOWNLOADS_PER_MIN` | 30 | public previews and downloads per IP |

Upload purpose `files` (private, any type). npm: `expo-document-picker` (mobile). Uses the storage additions `AnyType`, `PresignGetWith`, `Stat`, `ReadPrefix`, `Open`, `Put`, `Copy`, `NewKey`.

## Compliance

- Objects are keyed by the org, so org content survives its uploader. Account deletion: shared folders and items in others' drives stay (name cleared); each top-level My drive item anyone else can open (a grant to a person, team or the org anywhere inside) becomes org-owned under Shared folders, managed by org owners and admins; private ones go to the org's trash for `FILES_DELETED_ACCOUNT_KEEP_DAYS` (restorable by org admins), then are purged with their objects. Stars, recents, uploads in flight, zip links and connected download links are deleted. Shortcuts are items like any other (they hold a reference, no platform content); nothing else from connected platforms is stored.
- Leaving an org ends access; your items stay and org admins manage them (access's rule).
- Export: items created or owned (metadata), versions uploaded, stars, activity.
- UGC: public links serve user content to anyone: Report on public pages, the moderation queue, staff disable, per-IP limits, `noindex`, attachments for script-capable types.

## Stitching

1. Decide who creates shared folders (`files.create_shared_folders`, Settings → Permissions).
2. Set the quota, upload limit and `FILES_DELETED_ACCOUNT_KEEP_DAYS`, and say what happens to a leaver's files in the privacy policy.
3. Production bucket: `files/` private; CORS allows PUT from the web origin. Nothing else is public.
4. Navigation: web nav + Home tile, mobile Home entry; make it a mobile tab if central.
5. Link to items with `files.ItemLink(id)`; `/f/(.*)` is wired into `web/proxy.ts`.

## Connected platforms

Needs the `connectors` feature (≥ 2.0, `app.Connections`); without it Files offers no connected sources and works as before. Files keeps its own adapters (`connected_google.go`: Drive v3; `connected_graph.go`: Microsoft Graph) and gets each person's signing, host-locked client from `Connections.Client`; URLs come from `Connection.Endpoints` (`drive`, `drive_upload`, else `api` + `/drive/v3`; `graph`, else `api`).

| Capability | Scopes | Gives |
|---|---|---|
| `files.google.drive` | `drive.readonly` (drive.file reaches only app-made files) | My Drive, Shared with me, shared drives (`supportsAllDrives`); previews, downloads, search, sharing summary, Copy into Files, shortcuts |
| `files.google.drive.write` | `drive` | upload (resumable), new folder, rename, move within a drive, new version, trash, Save to |
| `files.microsoft.onedrive` | `Files.Read.All` (needed to open items others share, e.g. shortcuts) | OneDrive |
| `files.microsoft.onedrive.write` | `Files.ReadWrite.All` | changes as above (upload sessions) |
| `files.microsoft.sharepoint` | `Sites.Read.All`, `Files.Read.All` | libraries of followed sites, Microsoft Search |
| `files.microsoft.sharepoint.write` | `Sites.Read.All`, `Files.ReadWrite.All` | changes in libraries |

- Live and per person: nothing from a platform is stored except shortcut references; item metadata is cached 30 s per person and connection. Previews and downloads stream through `/files-connected/{token}` (15 minutes; the token never reaches the platform, the person's token never reaches the browser); Graph's pre-authenticated links are fetched without the token. Google Docs/Sheets/Slides/Drawings preview as exported PDF and download/copy as Office files (Forms, Sites… open on the platform only); Office files on Microsoft preview through Graph's PDF conversion; native documents are read-only here with Open in Google Docs.
- Writes: browser uploads go straight to the platform's resumable session (Drive chunks × 256 KiB, Graph × 320 KiB) and resume per chunk; moves stay inside one drive; trash goes to the platform's trash or recycle bin. Name clashes (Graph 409; Drive checked first), changed-since (Graph eTag / Drive version), no permission, rate limits and reconnects come back as `name_taken`, `target_changed`, `connected_forbidden`, `connection_rate_limited`, `connection_needs_auth`/`connection_scope_missing` (409, `details.fix_url`), never 401.
- Sharing: the app's Share… doesn't apply; ⋮ offers Manage sharing in Google Drive / OneDrive (opens the platform) and the details panel shows the platform's sharing read-only when the account may see it.
- Bridges: Copy into Files (the copy is app content); Add shortcut to Files (type `link`; whoever opens it is checked live with their own connection: Connect … to open, Reconnect, Ask for access in …; the pinner's connection is never used); Save to Google Drive / OneDrive from any Files file.
- Search: `Search` adds each active connection's own search, run in parallel, 3 s each (`Kind` `files.external`, `Context` "Google Drive · sam@acme.com", `Source`, no `OrgID`).
- Agents: `files_search` takes `source` (all · app · connected · a connection id); `files_connected_list`, `files_connected_get_item`, `files_connected_read_text` read; writes are separate tools per platform (`files_google_drive_*`, `files_onedrive_*`: create text file, replace lines in plain-text files, rename, move) with `Source` set, Target `files.external` `<connection id>:<platform id>`, exact Preview, and the connection's agent scope (`app.ConnectionScope`) checked in Preview (suggest) and Handler (edit); reads drop rows the connection's scope hides.
- Out of scope: revisions/version history and activity of connected files, restoring from the platform trash, moves between drives, folder uploads and zip downloads to/from connected drives, platform comments, Drive shortcuts' own creation, change notifications (webhooks; lists refresh on focus and writes), offline.

## Extending

```go
files.RegisterItemType(files.ItemType{Type: "repo", Title: "Repository", Icon: "git-branch",
    Link: func(id uuid.UUID) string { return "/repos/" + id.String() },
    CreatePath: "/repos/new?folder={folder}", ConvertPath: "/repos/new?from={folder}",
    OnDelete: func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error { /* drop the repo */ }})
it, err := files.CreateItem(ctx, tx, files.NewItem{OrgID: org, ParentID: &folder, Type: "repo", Name: "api", CreatedBy: me})
it, err = files.GetItem(ctx, q, me, id)              // 404 without view; agent-capped via ctx
it, err = files.RenameItem(ctx, tx, me, id, "api-v2") // 409 name_taken
err = files.TrashItem(ctx, tx, me, id)
err = files.WalkFolder(ctx, q, me, folder, func(e files.Entry) error { /* e.Path "docs/plan.md" */ })
rc, it, err := files.OpenFile(ctx, me, fileID)       // current version bytes
```

- Registered types show in listings, search, Recent and Starred with their icon, open `Link`, take the ⋮ menu, move, rename, star, trash and share like files; New offers `CreatePath`, folder menus `ConvertPath`. They aren't copied or zipped.
- Sharing writes go through access (≥ 1.1.0): a new shared folder's creator grant (`access.AddGrant`), the owner change on a cross-drive move and an account handover (`access.SetOwnerTree`), and the seed's grants and public link (`AddGrant`, `CreateLink`). files only reads access tables (lists, search, the admin view).
- Out of scope: office previews, thumbnails generated server side (images preview at full size), resumable multipart uploads over 5 GB, comments, file locking, offline file sync on mobile, a native public-link screen, content search beyond small text files.

## Provenance

New in the forum belt, modelled on Google Drive (My drive, shared drives, Shared with me, trash, version history) and Dropbox (upload tray, numbered names, folder upload). Sharing, inheritance and public links are the access feature's; the content-type and attachment rules follow the OWASP file-upload guidance.

## Changes

- 1.2.0: connected drives (needs connectors ≥ 2.0 for connections; works without): Google Drive incl. shared drives, OneDrive and SharePoint browsed, previewed and changed live with each person's connection; Copy into Files, shortcuts (new item type `link`, `files_links`), Save to Google Drive / OneDrive; connected search hits; agent tools with `source` and per-platform write tools; `GET /v1/files/sources`. Migration 1701. Verified against mock Drive v3 and Graph APIs only.
- 1.1.3: errors, the shared-folders permission, notifications and agent tool schemas name organizations with `orgs.Noun()` (orgs ≥ 1.3.0). The S3 paths (presigned upload, completion sniffing, forced-disposition downloads, versions, copies, empty files, zips, public links, purge and account deletion) ran against MinIO.
- 1.1.2: copy names organizations with orgs' noun (`orgsNoun`…, orgs ≥ 1.2.0). Search results carry `OrgID` and `Context` (org · folder or drive section) and honour the `org_id` scope (search ≥ 1.1.0).

- 1.1.1: agents can't move an item somewhere that changes who can open it (between folders whose inherited grants, owners or public links differ, such as a private file into a publicly linked folder) or suggest such a move (403 `agent_forbidden`); people still can. Tool `Target`s return `files.item` for `{}`; a text file an agent creates in a folder is stored under the folder's org.
- 1.1.0: no direct writes to access tables: the shared folder creator's manage grant, cross-drive moves, account-deletion handover and the seed use `access.AddGrant`, `SetOwnerTree` and `CreateLink` (needs access 1.1.0). No change for people using it; the seed now applies the share dialog's checks.
- 1.0.0: first release (backend, storage additions, shared hooks, web drive and public page, mobile, admin, agent tools, search).
