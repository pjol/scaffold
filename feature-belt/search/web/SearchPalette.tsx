"use client";

import * as DialogPrimitive from "@radix-ui/react-dialog";
import {
  errorMessage,
  groupSearchResultsByFeature,
  parseSearchOperators,
  SEARCH_OPERATOR_HINTS,
  searchQueryReady,
  searchResultContext,
  searchSourceOptions,
  skippedSearchProviders,
  unansweredSearchProviders,
  useDebouncedValue,
  useMe,
  useRemoveSavedSearch,
  useSavedSearches,
  useSaveSearch,
  useSearch,
  useSearchScope,
  type SearchResult,
  type SearchSaved,
} from "@scaffoldapp/shared";
import { AnimatePresence, Button, cn, motion, Skeleton, SourceBadge, SourceFilter, Spinner, ToggleChip, toast, useMotionTokens } from "@scaffoldapp/ui-web";
import { ArrowRight, Bookmark, BookmarkPlus, Clock, CornerDownLeft, Filter, Info, SearchX, Settings, TriangleAlert, X, type LucideIcon } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { featureNav, quickActions, settingsSections, type QuickAction } from "@/features/registry";
import { APP_NAME } from "@/lib/app";
import { formatRelative } from "@/lib/format";
import { SearchIcon, searchIcon } from "./icons";
import { Highlighted, OperatorChips } from "./parts";
import { useRecentSearches } from "./recent";
import { setSearchPaletteOpen, useSearchPaletteOpen } from "./store";

/** Hits shown per feature in the palette before "See all". */
const PER_GROUP = 4;

/** A command or destination in the palette: quick actions, the nav and settings pages. */
interface Command {
  id: string;
  label: string;
  icon: LucideIcon;
  keywords: string[];
  href?: string;
  run?: QuickAction["run"];
  withQuery?: boolean;
  group: "Actions" | "Go to";
}

/** Every command, in order: the features' quick actions, then where you can go. */
function useCommands(): Command[] {
  // useAvailable hooks are cheap cache reads; the list is static, so hook order is stable.
  const available = quickActions.map((a) => (a.useAvailable ? a.useAvailable() : true));
  return useMemo(() => {
    const out: Command[] = [];
    quickActions.forEach((a, i) => {
      if (!available[i]) return;
      out.push({ id: `action:${a.id}`, label: a.label, icon: a.icon, keywords: a.keywords ?? [], ...(a.href ? { href: a.href } : {}), ...(a.run ? { run: a.run } : {}), ...(a.withQuery ? { withQuery: true } : {}), group: "Actions" });
    });
    for (const n of featureNav) out.push({ id: `nav:${n.href}`, label: n.label, icon: n.icon, keywords: [], href: n.href, group: "Go to" });
    for (const s of settingsSections) out.push({ id: `settings:${s.href}`, label: `Settings: ${s.label}`, icon: s.icon, keywords: ["settings", "preferences"], href: s.href, group: "Go to" });
    out.push({ id: "settings", label: "Settings", icon: Settings, keywords: ["preferences", "account"], href: "/settings", group: "Go to" });
    return out;
  }, available);
}

/** Commands whose label or keywords start a word with each typed word. */
function matchCommands(commands: Command[], text: string): Command[] {
  const words = text.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return [];
  return commands.filter((c) => {
    if (c.withQuery) return false;
    const hay = [c.label, ...c.keywords].join(" ").toLowerCase();
    return words.every((w) => hay.split(/[\s:]+/).some((h) => h.startsWith(w)));
  });
}

/** True on Apple platforms, where the shortcut reads ⌘K. */
export function isMac(): boolean {
  return typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
}

/**
 * The command palette, mounted once while signed in (registry `sessionMounts`): ⌘K / Ctrl+K
 * anywhere (or the top-bar button) opens it. With no query: quick actions (registry
 * `quickActions`), saved searches, recent searches (this browser only) and the filters you can
 * type. Typing: matching actions and pages to go to, then results grouped by feature (4 each,
 * "See all"), "ask" actions with the text, the full results page and "Save this search".
 * Operators (from:, in:, type:, dates, is:) show as removable chips; words are highlighted.
 * Arrows move, Enter opens, ⌘/Ctrl+Enter opens in a new tab, Esc closes. With an org scope
 * registered (`registerSearchScope`), it searches that org and offers a toggle for every org.
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

type Row =
  | { type: "recent"; query: string }
  | { type: "saved"; saved: SearchSaved }
  | { type: "hint"; example: string }
  | { type: "command"; command: Command }
  | { type: "result"; item: SearchResult }
  | { type: "more"; feature: string }
  | { type: "save" }
  | { type: "all" };

function Palette() {
  const t = useMotionTokens();
  const router = useRouter();
  const me = useMe();
  const { recent, remember, forget, clear } = useRecentSearches(me.data?.id);
  const saved = useSavedSearches();
  const save = useSaveSearch();
  const removeSaved = useRemoveSavedSearch();
  const commands = useCommands();
  const [query, setQuery] = useState("");
  const debounced = useDebouncedValue(query, 180);
  const q = debounced.trim();
  const searchable = searchQueryReady(q);
  const typed = query.trim();
  const words = parseSearchOperators(typed).text;
  const scope = useSearchScope();
  const [everywhere, setEverywhere] = useState(false);
  const orgId = scope && !everywhere ? scope.id : undefined;
  const [source, setSource] = useState("all");
  const search = useSearch({ q, limit: 30, orgId, ...(source !== "all" ? { source } : {}) }, { enabled: searchable });
  const sourceOptions = useMemo(() => searchSourceOptions(search.data?.sources, APP_NAME), [search.data?.sources]);
  const groups = useMemo(
    () => (searchable && search.data ? groupSearchResultsByFeature(search.data.items, search.data.providers) : []),
    [searchable, search.data],
  );
  const matched = useMemo(() => matchCommands(commands, words).slice(0, 5), [commands, words]);
  const askers = useMemo(() => commands.filter((c) => c.withQuery), [commands]);
  const savedList = saved.data?.items ?? [];
  const alreadySaved = savedList.some((x) => x.query === typed && !x.kinds && !x.source);
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
  const setQueryFocus = (next: string) => {
    setQuery(next);
    inputRef.current?.focus();
  };

  const rows: Row[] = useMemo(() => {
    if (!typed) {
      return [
        ...commands.filter((c) => c.group === "Actions" && !c.withQuery).slice(0, 5).map((command) => ({ type: "command" as const, command })),
        ...savedList.slice(0, 5).map((s) => ({ type: "saved" as const, saved: s })),
        ...recent.map((r) => ({ type: "recent" as const, query: r })),
        ...SEARCH_OPERATOR_HINTS.map((h) => ({ type: "hint" as const, example: h.example })),
      ];
    }
    const out: Row[] = matched.map((command) => ({ type: "command" as const, command }));
    for (const g of groups) {
      for (const item of g.items.slice(0, PER_GROUP)) out.push({ type: "result", item });
      if (g.total > PER_GROUP) out.push({ type: "more", feature: g.feature });
    }
    if (words) for (const command of askers) out.push({ type: "command", command });
    if (groups.length) out.push({ type: "all" });
    if (searchable && search.data && !alreadySaved) out.push({ type: "save" });
    return out;
  }, [typed, words, commands, savedList, recent, matched, groups, askers, searchable, search.data, alreadySaved]);

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
  const runCommand = (c: Command, newTab: boolean) => {
    if (c.href) return go(c.href, newTab);
    close();
    c.run?.({ query: words || typed, navigate: (href) => router.push(href) });
  };
  const choose = (row: Row | undefined, newTab = false) => {
    if (!row) return;
    switch (row.type) {
      case "recent":
        return setQueryFocus(row.query);
      case "saved":
        if (row.saved.kinds || row.saved.source) return go(savedHref(row.saved), newTab);
        return setQueryFocus(row.saved.query);
      case "hint":
        return setQueryFocus(`${typed ? `${typed} ` : ""}${row.example}`);
      case "command":
        return runCommand(row.command, newTab);
      case "result":
        return go(row.item.link, newTab);
      case "more":
        return go(allResultsHref(query, scope && everywhere, source, row.feature), newTab);
      case "save":
        return save.mutate(
          { name: typed, query: typed },
          { onSuccess: () => toast.success("Search saved."), onError: (e) => toast.error(errorMessage(e, "Couldn't save the search.")) },
        );
      case "all":
        return go(allResultsHref(query, scope && everywhere, source), newTab);
    }
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
  const skipped = search.data ? skippedSearchProviders(search.data.providers) : [];
  const typing = typed.length > 0 && searchQueryReady(typed) && (!searchable || search.isPending || (search.isFetching && search.isPlaceholderData));
  const indexOf = (pred: (r: Row) => boolean) => rows.findIndex(pred);
  const option = (index: number, children: ReactNode, key: string) => (
    <Option key={key} id={optionId(index)} index={index} active={active === index} onHover={setActive} onChoose={(newTab) => choose(rows[index], newTab)}>
      {children}
    </Option>
  );
  const commandRows = (label: string, filter: (r: Row) => boolean) => {
    const list = rows.map((r, i) => [r, i] as const).filter(([r]) => filter(r));
    if (!list.length) return null;
    return (
      <Group label={label}>
        {list.map(([r, i]) => {
          if (r.type !== "command") return null;
          const Icon = r.command.icon;
          return option(
            i,
            <>
              <Icon aria-hidden className="size-5 shrink-0 text-fg-muted" />
              <span className="min-w-0 flex-1 truncate type-body text-fg">{r.command.withQuery ? `${r.command.label}: “${words}”` : r.command.label}</span>
              {r.command.group === "Go to" ? <span className="shrink-0 type-caption text-fg-subtle">Go to</span> : null}
            </>,
            r.command.id,
          );
        })}
      </Group>
    );
  };

  let body: ReactNode;
  if (!typed) {
    const savedRows = rows.map((r, i) => [r, i] as const).filter(([r]) => r.type === "saved");
    const recentRows = rows.map((r, i) => [r, i] as const).filter(([r]) => r.type === "recent");
    const hintRows = rows.map((r, i) => [r, i] as const).filter(([r]) => r.type === "hint");
    body = (
      <>
        {commandRows("Actions", (r) => r.type === "command")}
        {savedRows.length ? (
          <Group label="Saved searches">
            {savedRows.map(([r, i]) =>
              r.type === "saved"
                ? option(
                    i,
                    <>
                      <Bookmark aria-hidden className="size-4 shrink-0 text-fg-muted" />
                      <span className="min-w-0 flex-1 truncate type-body text-fg">{r.saved.name}</span>
                      <span
                        role="button"
                        tabIndex={-1}
                        aria-label={`Remove saved search ${r.saved.name}`}
                        onClick={(e) => {
                          e.stopPropagation();
                          removeSaved.mutate(r.saved.id, { onError: (err) => toast.error(errorMessage(err)) });
                        }}
                        className="flex size-6 items-center justify-center rounded-sm text-fg-subtle hover:text-fg"
                      >
                        <X aria-hidden className="size-4" />
                      </span>
                    </>,
                    r.saved.id,
                  )
                : null,
            )}
          </Group>
        ) : null}
        {recentRows.length ? (
          <Group
            label="Recent"
            action={
              <button type="button" onClick={clear} className="min-h-6 rounded-sm px-1 type-caption text-fg-muted hover:text-fg focus-visible:outline-2 focus-visible:outline-ring">
                Clear
              </button>
            }
          >
            {recentRows.map(([r, i]) =>
              r.type === "recent"
                ? option(
                    i,
                    <>
                      <Clock aria-hidden className="size-4 shrink-0 text-fg-muted" />
                      <span className="min-w-0 flex-1 truncate type-body text-fg">{r.query}</span>
                      <span
                        role="button"
                        tabIndex={-1}
                        aria-label={`Remove ${r.query} from recent searches`}
                        onClick={(e) => {
                          e.stopPropagation();
                          forget(r.query);
                        }}
                        className="flex size-6 items-center justify-center rounded-sm text-fg-subtle hover:text-fg"
                      >
                        <X aria-hidden className="size-4" />
                      </span>
                    </>,
                    r.query,
                  )
                : null,
            )}
          </Group>
        ) : null}
        <Group label="Filters">
          {hintRows.map(([r, i]) => {
            if (r.type !== "hint") return null;
            const hint = SEARCH_OPERATOR_HINTS.find((h) => h.example === r.example);
            return option(
              i,
              <>
                <Filter aria-hidden className="size-4 shrink-0 text-fg-muted" />
                <code className="shrink-0 rounded-xs bg-sunken px-1 type-label text-fg">{r.example}</code>
                <span className="min-w-0 flex-1 truncate type-label text-fg-muted">{hint?.label}</span>
              </>,
              r.example,
            );
          })}
        </Group>
      </>
    );
  } else if (!searchQueryReady(typed)) {
    body = (
      <>
        {commandRows("Go to", (r) => r.type === "command")}
        <p className="px-4 py-8 text-center type-body text-fg-muted">Type 2 or more characters, or a filter like from:@name</p>
      </>
    );
  } else if (search.isError && !search.data) {
    body = (
      <>
        {commandRows("Go to", (r) => r.type === "command" && !r.command.withQuery)}
        <div role="alert" className="flex flex-col items-center gap-3 px-4 py-8 text-center">
          <p className="type-body text-fg">Couldn&apos;t search. {errorMessage(search.error, "Check your connection and try again.")}</p>
          <Button size="sm" variant="secondary" onClick={() => void search.refetch()} loading={search.isFetching}>
            Retry
          </Button>
        </div>
      </>
    );
  } else if (!search.data) {
    body = (
      <>
        {commandRows("Go to", (r) => r.type === "command" && !r.command.withQuery)}
        <div className="flex flex-col gap-1 p-2" aria-label="Searching" role="progressbar">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="flex h-12 items-center gap-3 px-2">
              <Skeleton className="size-5 rounded-sm" />
              <Skeleton className="h-4 flex-1" />
            </div>
          ))}
        </div>
      </>
    );
  } else {
    body = (
      <>
        {commandRows("Go to", (r) => r.type === "command" && !r.command.withQuery)}
        {groups.length === 0 ? (
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
        ) : (
          groups.map((g) => (
            <Group key={g.feature} label={g.label}>
              {g.items.slice(0, PER_GROUP).map((item) => {
                const index = indexOf((r) => r.type === "result" && r.item.kind === item.kind && r.item.id === item.id);
                const Icon = searchIcon(item.icon, item.kind);
                const where = searchResultContext(item, orgId);
                return option(
                  index,
                  <>
                    <Icon aria-hidden className="size-5 shrink-0 text-fg-muted" />
                    <span className="flex min-w-0 flex-1 flex-col py-1">
                      <Highlighted className="truncate type-body text-fg" text={item.title} query={q} />
                      {where || item.snippet ? (
                        <span className="truncate type-label text-fg-muted">
                          {where}
                          {where && item.snippet ? " · " : ""}
                          {item.snippet ? <Highlighted text={item.snippet} query={q} /> : null}
                        </span>
                      ) : null}
                    </span>
                    <SourceBadge source={item.source} className="hidden sm:inline-flex" />
                    <SourceBadge source={item.source} iconOnly className="sm:hidden" />
                    <span className="shrink-0 type-caption text-fg-subtle">{formatRelative(item.at)}</span>
                  </>,
                  `${item.kind}:${item.id}`,
                );
              })}
              {g.total > PER_GROUP
                ? option(
                    indexOf((r) => r.type === "more" && r.feature === g.feature),
                    <>
                      <ArrowRight aria-hidden className="size-4 shrink-0 text-fg-muted" />
                      <span className="min-w-0 flex-1 truncate type-label text-fg-muted">
                        See all {g.total >= 30 ? "30+" : g.total} in {g.label}
                      </span>
                    </>,
                    `more:${g.feature}`,
                  )
                : null}
            </Group>
          ))
        )}
        {commandRows("Ask", (r) => r.type === "command" && !!r.command.withQuery)}
      </>
    );
  }

  const allIndex = rows.findIndex((r) => r.type === "all");
  const saveIndex = rows.findIndex((r) => r.type === "save");

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
                placeholder={`Search ${APP_NAME} or type a command`}
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
            <OperatorChips query={query} filters={search.data?.filters} onChange={setQueryFocus} className="flex flex-wrap gap-2 border-b border-border px-4 py-2" />
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
            {skipped.length > 0 && searchable ? (
              <div role="status" className="flex items-center gap-2 border-b border-border px-4 py-2 type-label text-fg-muted">
                <Info aria-hidden className="size-4 shrink-0" />
                <span className="min-w-0 flex-1">Not searched: {skipped.join(", ")}.</span>
              </div>
            ) : null}
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
              {allIndex >= 0 || saveIndex >= 0 ? (
                <div className="px-2 pt-1">
                  {allIndex >= 0 ? (
                    <Option id={optionId(allIndex)} index={allIndex} active={active === allIndex} onHover={setActive} onChoose={(newTab) => choose(rows[allIndex], newTab)}>
                      <ArrowRight aria-hidden className="size-5 shrink-0 text-fg-muted" />
                      <span className="min-w-0 flex-1 truncate type-body text-fg">See all results for “{query.trim()}”</span>
                    </Option>
                  ) : null}
                  {saveIndex >= 0 ? (
                    <Option id={optionId(saveIndex)} index={saveIndex} active={active === saveIndex} onHover={setActive} onChoose={() => choose(rows[saveIndex])}>
                      <BookmarkPlus aria-hidden className="size-5 shrink-0 text-fg-muted" />
                      <span className="min-w-0 flex-1 truncate type-body text-fg">Save this search</span>
                    </Option>
                  ) : null}
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

/** The results page for a query; `everywhere` keeps the "every org" choice, `source` the source chip, `kind` one feature. */
function allResultsHref(query: string, everywhere: boolean | null, source = "all", kind?: string): string {
  return `/search?q=${encodeURIComponent(query.trim())}${everywhere ? "&scope=all" : ""}${source !== "all" ? `&source=${encodeURIComponent(source)}` : ""}${kind ? `&kind=${encodeURIComponent(kind)}` : ""}`;
}

/** A saved search with filters opens the results page with them. */
function savedHref(s: SearchSaved): string {
  return `/search?q=${encodeURIComponent(s.query)}${s.kinds ? `&kind=${encodeURIComponent(s.kinds.split(",")[0] ?? "")}` : ""}${s.source ? `&source=${encodeURIComponent(s.source)}` : ""}`;
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
