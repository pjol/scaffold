"use client";

import * as DialogPrimitive from "@radix-ui/react-dialog";
import {
  errorMessage,
  groupSearchResults,
  SEARCH_MIN_QUERY_CHARS,
  searchResultContext,
  searchSourceOptions,
  unansweredSearchProviders,
  useDebouncedValue,
  useMe,
  useSearch,
  useSearchScope,
  type SearchResult,
} from "@scaffoldapp/shared";
import { AnimatePresence, Button, cn, motion, Skeleton, SourceBadge, SourceFilter, Spinner, ToggleChip, useMotionTokens } from "@scaffoldapp/ui-web";
import { ArrowRight, Clock, CornerDownLeft, SearchX, TriangleAlert, X } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { APP_NAME } from "@/lib/app";
import { formatRelative } from "@/lib/format";
import { SearchIcon, searchIcon } from "./icons";
import { useRecentSearches } from "./recent";
import { setSearchPaletteOpen, useSearchPaletteOpen } from "./store";

/** True on Apple platforms, where the shortcut reads ⌘K. */
export function isMac(): boolean {
  return typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
}

/**
 * The command palette, mounted once while signed in (registry `sessionMounts`): ⌘K / Ctrl+K
 * anywhere (or the top-bar button) opens it. Results arrive grouped by kind; arrows move,
 * Enter opens, ⌘/Ctrl+Enter opens in a new tab, Esc closes. With no query it lists recent
 * searches (this browser only). The last row opens the full results page. With an org scope
 * registered (`registerSearchScope`), it searches that org and offers a toggle for every org;
 * each row says where the hit lives.
 */
export function SearchPalette() {
  const open = useSearchPaletteOpen();
  const pathname = usePathname();

  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setSearchPaletteOpen(!open);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  // Navigating anywhere closes it.
  useEffect(() => setSearchPaletteOpen(false), [pathname]);

  return (
    <DialogPrimitive.Root open={open} onOpenChange={setSearchPaletteOpen}>
      <AnimatePresence>{open ? <Palette key="palette" /> : null}</AnimatePresence>
    </DialogPrimitive.Root>
  );
}

type Row = { type: "recent"; query: string } | { type: "result"; item: SearchResult } | { type: "all" };

function Palette() {
  const t = useMotionTokens();
  const router = useRouter();
  const me = useMe();
  const { recent, remember, forget, clear } = useRecentSearches(me.data?.id);
  const [query, setQuery] = useState("");
  const debounced = useDebouncedValue(query, 180);
  const q = debounced.trim();
  const searchable = q.length >= SEARCH_MIN_QUERY_CHARS;
  const scope = useSearchScope();
  const [everywhere, setEverywhere] = useState(false);
  const orgId = scope && !everywhere ? scope.id : undefined;
  const [source, setSource] = useState("all");
  const search = useSearch({ q, limit: 30, orgId, ...(source !== "all" ? { source } : {}) }, { enabled: searchable });
  const sourceOptions = useMemo(() => searchSourceOptions(search.data?.sources, APP_NAME), [search.data?.sources]);
  const groups = useMemo(() => (searchable && search.data ? groupSearchResults(search.data.items) : []), [searchable, search.data]);
  const [active, setActive] = useState(0);
  const listId = useId();
  const listRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const scopeTo = (all: boolean) => {
    setEverywhere(all);
    inputRef.current?.focus(); // keep typing and arrow keys working
  };
  const sourceTo = (id: string) => {
    setSource(id);
    inputRef.current?.focus();
  };

  const rows: Row[] = useMemo(() => {
    if (query.trim().length < SEARCH_MIN_QUERY_CHARS) return recent.map((r) => ({ type: "recent", query: r }));
    const out: Row[] = groups.flatMap((g) => g.items.map((item) => ({ type: "result" as const, item })));
    if (out.length) out.push({ type: "all" });
    return out;
  }, [query, recent, groups, searchable]);

  useEffect(() => setActive(0), [q, rows.length]);
  useEffect(() => {
    listRef.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView({ block: "nearest" });
  }, [active]);

  const close = () => setSearchPaletteOpen(false);
  const go = (href: string, newTab: boolean) => {
    if (q) remember(q);
    if (newTab) {
      window.open(href, "_blank", "noopener");
      return;
    }
    close();
    router.push(href);
  };
  const choose = (row: Row | undefined, newTab = false) => {
    if (!row) return;
    if (row.type === "recent") setQuery(row.query);
    else if (row.type === "result") go(row.item.link, newTab);
    else go(allResultsHref(query, scope && everywhere, source), newTab);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    const n = rows.length;
    if (e.key === "ArrowDown" && n) {
      e.preventDefault();
      setActive((i) => (i + 1) % n);
    } else if (e.key === "ArrowUp" && n) {
      e.preventDefault();
      setActive((i) => (i - 1 + n) % n);
    } else if (e.key === "Home" && n && e.ctrlKey) {
      setActive(0);
    } else if (e.key === "End" && n && e.ctrlKey) {
      setActive(n - 1);
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (n) choose(rows[active], e.metaKey || e.ctrlKey);
      else if (searchable) go(allResultsHref(query, scope && everywhere, source), e.metaKey || e.ctrlKey);
    }
  };

  const optionId = (i: number) => `${listId}-opt-${i}`;
  const unanswered = search.data ? unansweredSearchProviders(search.data.providers) : [];
  const typing = query.trim().length >= SEARCH_MIN_QUERY_CHARS && (!searchable || search.isPending || (search.isFetching && search.isPlaceholderData));

  let body: ReactNode;
  if (query.trim().length < SEARCH_MIN_QUERY_CHARS) {
    body =
      recent.length === 0 ? (
        <p className="px-4 py-8 text-center type-body text-fg-muted">
          {query.trim() ? `Type ${SEARCH_MIN_QUERY_CHARS} or more characters` : `Search ${APP_NAME}`}
        </p>
      ) : (
        <Group
          label="Recent"
          action={
            <button type="button" onClick={clear} className="min-h-6 rounded-sm px-1 type-caption text-fg-muted hover:text-fg focus-visible:outline-2 focus-visible:outline-ring">
              Clear
            </button>
          }
        >
          {recent.map((r, i) => (
            <Option key={r} id={optionId(i)} index={i} active={active === i} onHover={setActive} onChoose={() => choose(rows[i])}>
              <Clock aria-hidden className="size-4 shrink-0 text-fg-muted" />
              <span className="min-w-0 flex-1 truncate type-body text-fg">{r}</span>
              <span
                role="button"
                tabIndex={-1}
                aria-label={`Remove ${r} from recent searches`}
                onClick={(e) => {
                  e.stopPropagation();
                  forget(r);
                }}
                className="flex size-6 items-center justify-center rounded-sm text-fg-subtle hover:text-fg"
              >
                <X aria-hidden className="size-4" />
              </span>
            </Option>
          ))}
        </Group>
      );
  } else if (search.isError && !search.data) {
    body = (
      <div role="alert" className="flex flex-col items-center gap-3 px-4 py-8 text-center">
        <p className="type-body text-fg">Couldn&apos;t search. {errorMessage(search.error, "Check your connection and try again.")}</p>
        <Button size="sm" variant="secondary" onClick={() => void search.refetch()} loading={search.isFetching}>
          Retry
        </Button>
      </div>
    );
  } else if (!search.data) {
    body = (
      <div className="flex flex-col gap-1 p-2" aria-label="Searching" role="progressbar">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="flex h-12 items-center gap-3 px-2">
            <Skeleton className="size-5 rounded-sm" />
            <Skeleton className="h-4 flex-1" />
          </div>
        ))}
      </div>
    );
  } else if (groups.length === 0) {
    body = (
      <div className="flex flex-col items-center gap-2 px-4 py-8 text-center">
        <SearchX aria-hidden className="size-6 text-fg-subtle" />
        <p className="type-body text-fg">No results for “{q}”</p>
        {source !== "all" ? (
          <Button size="sm" variant="secondary" onClick={() => sourceTo("all")}>
            Search every source
          </Button>
        ) : scope && !everywhere ? (
          <Button size="sm" variant="secondary" onClick={() => scopeTo(true)}>
            Search {scope.allLabel.charAt(0).toLowerCase() + scope.allLabel.slice(1)}
          </Button>
        ) : null}
      </div>
    );
  } else {
    let i = 0;
    body = groups.map((g) => (
      <Group key={g.kind} label={g.label}>
        {g.items.map((item) => {
          const index = i++;
          const Icon = searchIcon(item.icon, item.kind);
          const sub = [searchResultContext(item, orgId), item.snippet].filter(Boolean).join(" · ");
          return (
            <Option key={`${item.kind}:${item.id}`} id={optionId(index)} index={index} active={active === index} onHover={setActive} onChoose={(newTab) => choose(rows[index], newTab)}>
              <Icon aria-hidden className="size-5 shrink-0 text-fg-muted" />
              <span className="flex min-w-0 flex-1 flex-col py-1">
                <span className="truncate type-body text-fg">{item.title}</span>
                {sub ? <span className="truncate type-label text-fg-muted">{sub}</span> : null}
              </span>
              <SourceBadge source={item.source} className="hidden sm:inline-flex" />
              <SourceBadge source={item.source} iconOnly className="sm:hidden" />
              <span className="shrink-0 type-caption text-fg-subtle">{formatRelative(item.at)}</span>
            </Option>
          );
        })}
      </Group>
    ));
  }

  const allIndex = rows.findIndex((r) => r.type === "all");

  return (
    <DialogPrimitive.Portal forceMount>
      <DialogPrimitive.Overlay asChild forceMount>
        <motion.div
          className="fixed inset-0 z-(--app-z-overlay) bg-overlay"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: t.duration.fast }}
        />
      </DialogPrimitive.Overlay>
      <div className="pointer-events-none fixed inset-0 z-(--app-z-modal) flex items-start justify-center px-3 pt-3 sm:px-6 sm:pt-[12vh]">
        <DialogPrimitive.Content asChild forceMount aria-describedby={undefined}>
          <motion.div
            className="pointer-events-auto flex max-h-[min(80dvh,40rem)] w-full max-w-2xl flex-col overflow-hidden rounded-xl border border-border bg-surface-raised shadow-xl outline-none"
            initial={{ opacity: 0, scale: 0.98, y: -8 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.98, y: -4, transition: { duration: t.duration.fast } }}
            transition={t.spring.snappy}
          >
            <DialogPrimitive.Title className="sr-only">Search</DialogPrimitive.Title>
            <div className="flex items-center gap-3 border-b border-border px-4">
              <SearchIcon aria-hidden className="size-5 shrink-0 text-fg-muted" />
              <input
                ref={inputRef}
                autoFocus
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={onKeyDown}
                placeholder={`Search ${APP_NAME}`}
                role="combobox"
                aria-expanded={rows.length > 0}
                aria-controls={listId}
                aria-activedescendant={rows.length ? optionId(active) : undefined}
                aria-autocomplete="list"
                aria-label="Search"
                maxLength={200}
                enterKeyHint="search"
                className="h-14 min-w-0 flex-1 bg-transparent type-body text-fg outline-none placeholder:text-fg-subtle"
              />
              {typing ? <Spinner size={16} label="Searching" /> : null}
              <DialogPrimitive.Close className="flex size-10 items-center justify-center rounded-md text-fg-muted hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring" aria-label="Close search">
                <X aria-hidden className="size-5" />
              </DialogPrimitive.Close>
            </div>
            {scope ? (
              <div role="group" aria-label="Where to search" className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
                <ToggleChip pressed={!everywhere} onPressedChange={() => scopeTo(false)}>
                  <span className="max-w-48 truncate">{scope.name}</span>
                </ToggleChip>
                <ToggleChip pressed={everywhere} onPressedChange={() => scopeTo(true)}>
                  {scope.allLabel}
                </ToggleChip>
              </div>
            ) : null}
            {searchable ? <SourceFilter options={sourceOptions} value={source} onChange={sourceTo} label="Source" className="border-b border-border px-4 py-2" /> : null}
            {unanswered.length > 0 && searchable ? (
              <div role="status" className="flex items-center gap-2 border-b border-border bg-warning-soft px-4 py-2 type-label text-warning-strong">
                <TriangleAlert aria-hidden className="size-4 shrink-0" />
                <span className="min-w-0 flex-1">Some results may be missing: {unanswered.join(", ")} didn&apos;t answer.</span>
                <button type="button" className="min-h-6 font-semibold underline-offset-2 hover:underline" onClick={() => void search.refetch()}>
                  Retry
                </button>
              </div>
            ) : null}
            <div ref={listRef} id={listId} role="listbox" aria-label="Results" className="min-h-0 flex-1 overflow-y-auto overscroll-contain py-2">
              {body}
              {allIndex >= 0 ? (
                <div className="px-2 pt-1">
                  <Option id={optionId(allIndex)} index={allIndex} active={active === allIndex} onHover={setActive} onChoose={(newTab) => choose(rows[allIndex], newTab)}>
                    <ArrowRight aria-hidden className="size-5 shrink-0 text-fg-muted" />
                    <span className="min-w-0 flex-1 truncate type-body text-fg">See all results for “{query.trim()}”</span>
                  </Option>
                </div>
              ) : null}
            </div>
            <div className="hidden items-center gap-4 border-t border-border px-4 py-2 type-caption text-fg-subtle sm:pointer-fine:flex">
              <span>
                <Kbd>↑</Kbd> <Kbd>↓</Kbd> move
              </span>
              <span className="flex items-center gap-1">
                <Kbd>
                  <CornerDownLeft aria-hidden className="size-3" />
                </Kbd>
                open
              </span>
              <span>
                <Kbd>{isMac() ? "⌘" : "Ctrl"}</Kbd> <Kbd>↵</Kbd> new tab
              </span>
              <span>
                <Kbd>Esc</Kbd> close
              </span>
            </div>
          </motion.div>
        </DialogPrimitive.Content>
      </div>
    </DialogPrimitive.Portal>
  );
}

/** The results page for a query; `everywhere` keeps the "every org" choice, `source` the source chip. */
function allResultsHref(query: string, everywhere: boolean | null, source = "all"): string {
  return `/search?q=${encodeURIComponent(query.trim())}${everywhere ? "&scope=all" : ""}${source !== "all" ? `&source=${encodeURIComponent(source)}` : ""}`;
}

function Group({ label, action, children }: { label: string; action?: ReactNode; children: ReactNode }) {
  return (
    <div role="group" aria-label={label} className="flex flex-col px-2 pb-2">
      <div className="flex h-8 items-center justify-between px-2">
        <span className="type-caption font-semibold text-fg-muted">{label}</span>
        {action}
      </div>
      {children}
    </div>
  );
}

function Option({
  id,
  index,
  active,
  onHover,
  onChoose,
  children,
}: {
  id: string;
  index: number;
  active: boolean;
  onHover: (i: number) => void;
  onChoose: (newTab: boolean) => void;
  children: ReactNode;
}) {
  return (
    <div
      id={id}
      role="option"
      aria-selected={active}
      data-index={index}
      onMouseMove={() => !active && onHover(index)}
      onMouseDown={(e) => e.preventDefault()}
      onClick={(e) => onChoose(e.metaKey || e.ctrlKey)}
      className={cn("flex min-h-12 cursor-pointer items-center gap-3 rounded-md px-2", active ? "bg-sunken" : "")}
    >
      {children}
    </div>
  );
}

function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="inline-flex min-w-5 items-center justify-center rounded-xs border border-border px-1 font-sans text-fg-muted">{children}</kbd>;
}
