"use client";

import {
  connectionFixPath,
  docsPlatformName,
  docsTitle,
  errorMessage,
  isConnectionErrorCode,
  useDocsConnectedSearch,
  useDocsConnectedTree,
  useDocsSources,
  type DocsConnectedItem,
  type DocsConnectedSource,
} from "@scaffoldapp/shared";
import { ConnectPrompt, ConnectedSection, Input, SegmentedControl, Skeleton, SourceBadge, cn, connectedSectionItems, withReturnTo } from "@scaffoldapp/ui-web";
import { ChevronDown, ChevronRight, FileText, Search, Table2 } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";

const DISMISS_KEY = "scaffoldapp.docs.connect-dismissed";

function activeOf(pathname: string): { conn: string | null; ref: string | null } {
  const m = /^\/docs\/c\/([^/]+)(?:\/([^/]+))?/.exec(pathname);
  return { conn: m?.[1] ? decodeURIComponent(m[1]) : null, ref: m?.[2] ? decodeURIComponent(m[2]) : null };
}

function ItemRow({ item, depth, activeRef, expandable }: { item: DocsConnectedItem; depth: number; activeRef: string | null; expandable: boolean }) {
  const [open, setOpen] = useState(false);
  const kids = useDocsConnectedTree(item.connection_id, { parent: item.id, enabled: open });
  const active = activeRef === item.id;
  const canOpen = expandable && item.kind === "page";
  return (
    <li role="treeitem" aria-expanded={canOpen ? open : undefined} aria-selected={active} className="flex flex-col">
      <div
        style={{ paddingLeft: `${depth * 12 + 4}px` }}
        className={cn("group/row flex min-h-8 items-center gap-0.5 rounded-sm pr-1 pointer-coarse:min-h-tap", active ? "bg-sunken font-medium text-fg" : "text-fg-muted hover:bg-sunken hover:text-fg")}
      >
        <button
          type="button"
          aria-label={open ? `Collapse ${docsTitle(item.title)}` : `Expand ${docsTitle(item.title)}`}
          className={cn("flex size-6 shrink-0 items-center justify-center rounded-sm hover:bg-surface focus-visible:outline-2 focus-visible:outline-ring [&_svg]:size-4", !canOpen && "invisible")}
          onClick={() => setOpen((o) => !o)}
        >
          {open ? <ChevronDown aria-hidden /> : <ChevronRight aria-hidden />}
        </button>
        <Link href={item.link} aria-current={active ? "page" : undefined} className="flex min-w-0 flex-1 items-center gap-2 rounded-sm py-1 focus-visible:outline-2 focus-visible:outline-ring">
          <span aria-hidden className="flex size-5 shrink-0 items-center justify-center">
            {item.icon ?? (item.kind === "database" ? <Table2 className="size-4" /> : <FileText className="size-4" />)}
          </span>
          <span className="truncate type-label">{docsTitle(item.title)}</span>
        </Link>
        <SourceBadge source={item.source} iconOnly className="opacity-0 group-hover/row:opacity-100 group-focus-within/row:opacity-100" />
      </div>
      {open ? (
        kids.isPending ? (
          <Skeleton className="my-1 h-6" style={{ marginLeft: `${(depth + 1) * 12 + 28}px` }} />
        ) : kids.data && kids.data.items.length > 0 ? (
          <ul role="group" className="flex flex-col">
            {kids.data.items.map((c) => (
              <ItemRow key={c.id} item={c} depth={depth + 1} activeRef={activeRef} expandable />
            ))}
          </ul>
        ) : (
          <p className="py-1 type-caption text-fg-subtle" style={{ paddingLeft: `${(depth + 1) * 12 + 32}px` }}>
            No pages inside
          </p>
        )
      ) : null}
    </li>
  );
}

function List({ source, section, activeRef }: { source: DocsConnectedSource; section?: "recent" | "shared"; activeRef: string | null }) {
  const pathname = usePathname() ?? "/docs";
  const tree = useDocsConnectedTree(source.id, section ? { section } : {});
  const platform = docsPlatformName(source.provider);
  if (tree.isPending) {
    return (
      <div className="flex flex-col gap-1 px-2 py-1" aria-busy>
        <Skeleton className="h-6" />
        <Skeleton className="h-6 w-3/4" />
      </div>
    );
  }
  if (tree.isError) {
    const fix = (tree.error.details?.["fix_url"] as string | undefined) ?? (isConnectionErrorCode(tree.error.code) ? connectionFixPath(source.capability) : undefined);
    return (
      <p className="px-3 py-1 type-caption text-fg-muted">
        {errorMessage(tree.error, `Couldn't load ${platform}.`)}{" "}
        {fix ? (
          <Link href={withReturnTo(fix, pathname)} className="font-semibold text-primary-strong hover:underline">
            Fix it
          </Link>
        ) : (
          <button type="button" className="font-semibold text-primary-strong hover:underline" onClick={() => void tree.refetch()}>
            Try again
          </button>
        )}
      </p>
    );
  }
  const items = tree.data.items;
  if (items.length === 0) {
    return (
      <p className="px-3 py-1 type-caption text-fg-subtle">
        {source.provider === "notion"
          ? "No pages shared with this app yet. In Notion, open a page's ••• menu → Connections and add it."
          : section === "shared"
            ? "Nothing shared with you yet."
            : "No documents yet."}
      </p>
    );
  }
  return (
    <ul role="tree" aria-label={`${platform} pages`} className="flex flex-col">
      {items.map((it) => (
        <ItemRow key={it.id} item={it} depth={0} activeRef={activeRef} expandable={source.provider === "notion"} />
      ))}
      {tree.hasNextPage ? (
        <li>
          <button type="button" className="w-full rounded-sm px-3 py-1 text-left type-caption text-fg-muted hover:bg-sunken hover:text-fg" onClick={() => void tree.fetchNextPage()}>
            {tree.isFetchingNextPage ? "Loading…" : "Show more"}
          </button>
        </li>
      ) : null}
    </ul>
  );
}

/** One connected account opened in the sidebar: search, then the tree (Notion) or Recent / Shared with me (Google). */
function Browser({ source, activeRef }: { source: DocsConnectedSource; activeRef: string | null }) {
  const [q, setQ] = useState("");
  const [section, setSection] = useState<"recent" | "shared">("recent");
  const results = useDocsConnectedSearch(source.id, q);
  const platform = docsPlatformName(source.provider);
  return (
    <div className="flex flex-col gap-1 pt-1 pl-2">
      <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={`Search ${platform}`} aria-label={`Search ${platform} · ${source.account ?? ""}`} leadingIcon={<Search />} className="h-8" />
      {q.trim() ? (
        results.isPending ? (
          <Skeleton className="mx-2 my-1 h-6" />
        ) : results.isError ? (
          <p className="px-3 py-1 type-caption text-fg-muted">{errorMessage(results.error, `Couldn't search ${platform}.`)}</p>
        ) : results.data.length === 0 ? (
          <p className="px-3 py-1 type-caption text-fg-subtle">Nothing matches in {platform}.</p>
        ) : (
          <ul role="tree" aria-label="Search results" className="flex flex-col">
            {results.data.map((it) => (
              <ItemRow key={it.id} item={it} depth={0} activeRef={activeRef} expandable={false} />
            ))}
          </ul>
        )
      ) : source.provider === "google" ? (
        <>
          <SegmentedControl<"recent" | "shared">
            aria-label="Which documents"
            value={section}
            onValueChange={setSection}
            options={[
              { value: "recent", label: "Recent" },
              { value: "shared", label: "Shared with me" },
            ]}
          />
          <List source={source} section={section} activeRef={activeRef} />
        </>
      ) : (
        <List source={source} activeRef={activeRef} />
      )}
    </div>
  );
}

/**
 * The sidebar's "Connected" section: each connected Notion workspace and Google account
 * (with its status and a Reconnect link), opening into its pages. With nothing connected, a
 * quiet prompt offers Notion and Google Docs once (dismissable).
 */
export function ConnectedDocsSection({ orgId }: { orgId: string }) {
  const pathname = usePathname() ?? "/docs";
  const sources = useDocsSources(orgId);
  const { conn: activeConn, ref: activeRef } = activeOf(pathname);
  const [open, setOpen] = useState<string | null>(null);
  const [dismissed, setDismissed] = useState(true);
  useEffect(() => {
    if (activeConn) setOpen(activeConn);
  }, [activeConn]);
  useEffect(() => {
    try {
      setDismissed(window.localStorage.getItem(`${DISMISS_KEY}.${orgId}`) === "1");
    } catch {
      setDismissed(false);
    }
  }, [orgId]);
  const connected = (sources.data?.sources ?? []).filter((s) => s.type === "connected");
  const offers = sources.data?.offers ?? [];
  if (sources.isError) return null;
  if (!sources.isPending && connected.length === 0) {
    if (dismissed || offers.length === 0) return null;
    return (
      <ConnectPrompt
        offers={offers}
        returnTo={pathname}
        title="Work with Notion and Google Docs here"
        info="Read and edit Notion pages and Google Docs in Docs with your own account. Notion shows only the pages you pick when connecting."
        linkAs={Link}
        onDismiss={() => {
          setDismissed(true);
          try {
            window.localStorage.setItem(`${DISMISS_KEY}.${orgId}`, "1");
          } catch {
            // Shown again next time in private mode.
          }
        }}
      />
    );
  }
  const current = connected.find((s) => s.id === open) as DocsConnectedSource | undefined;
  const addCapability = offers[0]?.capability ?? connected[0]?.capability;
  return (
    <div className="flex flex-col">
      <ConnectedSection
        items={connectedSectionItems(connected)}
        activeId={open}
        onSelect={(id) => setOpen((o) => (o === id ? null : id))}
        linkAs={Link}
        addHref={withReturnTo(offers[0]?.connect_url ?? connectionFixPath(addCapability), pathname)}
        loading={sources.isPending}
      />
      {current && current.status !== "blocked" ? <Browser key={current.id} source={current} activeRef={activeConn === current.id ? activeRef : null} /> : null}
    </div>
  );
}
