"use client";

import {
  errorMessage,
  groupSearchResultsByFeature,
  searchProviderLabel,
  searchQueryReady,
  searchResultContext,
  searchSourceOptions,
  skippedSearchProviders,
  sourceLabel,
  unansweredSearchProviders,
  useDebouncedValue,
  useMe,
  useRemoveSavedSearch,
  useSavedSearches,
  useSaveSearch,
  useSearch,
  useSearchScope,
  type SearchResult,
} from "@scaffoldapp/shared";
import { Banner, Button, Card, Divider, EmptyState, ErrorState, IconButton, Input, SkeletonGroup, SkeletonRow, SourceBadge, SourceFilter, ToggleChip, toast } from "@scaffoldapp/ui-web";
import { Bookmark, BookmarkPlus, Clock, SearchX, X } from "lucide-react";
import Link from "next/link";
import { Fragment, useEffect, useMemo, useState } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { APP_NAME } from "@/lib/app";
import { formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { SearchIcon, searchIcon } from "./icons";
import { Highlighted, OperatorChips } from "./parts";
import { useRecentSearches } from "./recent";

/**
 * /search?q=&kind=&scope=all&source=: every result for a query, grouped by feature with a
 * filter per feature found, operator chips (from:, in:, type:, dates, is:), highlighted words,
 * Save search, and source chips (All · the app · each connected platform with
 * hits); connected hits carry a SourceBadge. With an org scope registered, the current org by default and every org with
 * `scope=all`. The query and filters live in the URL (shareable, back works). Partial
 * answers say which feature didn't respond; skipped ones say which operator they can't apply.
 * Empty query: saved searches (every device) and recent ones (this browser).
 */
export function SearchScreen() {
  const [urlQ, setUrlQ] = useUrlState("q", "");
  const [kind, setKind] = useUrlState("kind", "");
  const [scopeParam, setScopeParam] = useUrlState("scope", "");
  const [sourceParam, setSourceParam] = useUrlState("source", "");
  const source = sourceParam || "all";
  const scope = useSearchScope();
  const everywhere = scopeParam === "all";
  const orgId = scope && !everywhere ? scope.id : undefined;
  const [text, setText] = useState(urlQ);
  const debounced = useDebouncedValue(text, 250);
  const me = useMe();
  const { recent, remember, forget, clear } = useRecentSearches(me.data?.id);
  const saved = useSavedSearches();
  const save = useSaveSearch();
  const removeSaved = useRemoveSavedSearch();

  useEffect(() => setText(urlQ), [urlQ]);
  useEffect(() => {
    const next = debounced.trim();
    if (next !== urlQ) setUrlQ(next || null);
  }, [debounced]);

  const q = urlQ.trim();
  const searchable = searchQueryReady(q);
  const sourceFilter = source !== "all" ? { source } : {};
  const all = useSearch({ q, limit: 50, orgId, ...sourceFilter }, { enabled: searchable });
  const filtered = useSearch({ q, kinds: kind ? [kind] : [], limit: 50, orgId, ...sourceFilter }, { enabled: searchable && !!kind });
  const res = kind ? filtered : all;
  const sourceOptions = useMemo(() => searchSourceOptions(res.data?.sources, APP_NAME), [res.data?.sources]);
  const groups = useMemo(() => (res.data ? groupSearchResultsByFeature(res.data.items, res.data.providers) : []), [res.data]);
  const kinds = useMemo(() => {
    const ks = all.data ? groupSearchResultsByFeature(all.data.items).map((g) => g.feature) : [];
    return kind && !ks.includes(kind) ? [...ks, kind] : ks;
  }, [all.data, kind]);
  const unanswered = res.data ? unansweredSearchProviders(res.data.providers) : [];
  const skipped = res.data ? skippedSearchProviders(res.data.providers) : [];
  const savedList = saved.data?.items ?? [];
  const isSaved = savedList.some((x) => x.query === q && x.kinds === kind && x.source === (source === "all" ? "" : source));

  useEffect(() => {
    if (searchable && res.data && res.data.query === q) remember(q);
  }, [res.data?.query]);

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <PageHeader title="Search" info={`Searches everything you can see in ${APP_NAME}. Press ⌘K or Ctrl+K anywhere to search without leaving the page.`} />
      <div className="flex flex-col gap-4">
        <form
          role="search"
          className="max-w-form"
          onSubmit={(e) => {
            e.preventDefault();
            setUrlQ(text.trim() || null);
          }}
        >
          <Input
            type="search"
            aria-label="Search"
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder={`Search ${APP_NAME}`}
            leadingIcon={<SearchIcon aria-hidden />}
            maxLength={200}
            enterKeyHint="search"
            autoFocus={!urlQ}
          />
        </form>
        <OperatorChips
          query={text}
          filters={res.data?.filters}
          onChange={(next) => {
            setText(next);
            setUrlQ(next || null);
          }}
        />
        {scope ? (
          <div className="flex flex-wrap gap-2" role="group" aria-label="Where to search">
            <ToggleChip pressed={!everywhere} onPressedChange={() => setScopeParam(null)}>
              <span className="max-w-48 truncate">{scope.name}</span>
            </ToggleChip>
            <ToggleChip pressed={everywhere} onPressedChange={() => setScopeParam("all")}>
              {scope.allLabel}
            </ToggleChip>
          </div>
        ) : null}
        {searchable ? <SourceFilter options={sourceOptions} value={source} onChange={(id) => setSourceParam(id === "all" ? null : id)} label="Source" /> : null}
        {searchable && kinds.length > 1 ? (
          <div className="flex flex-wrap gap-2" role="group" aria-label="Filter by feature">
            <ToggleChip pressed={!kind} onPressedChange={() => setKind(null)}>
              All
            </ToggleChip>
            {kinds.map((k) => (
              <ToggleChip key={k} pressed={kind === k} onPressedChange={(on) => setKind(on ? k : null)}>
                {searchProviderLabel(k)}
              </ToggleChip>
            ))}
          </div>
        ) : null}
      </div>

      {!q ? (
        recent.length || savedList.length ? (
          <div className="flex flex-col gap-section">
            {savedList.length ? (
              <Section title="Saved searches">
                <Card padding="none">
                  <ul>
                    {savedList.map((x, i) => (
                      <Fragment key={x.id}>
                        {i > 0 ? <Divider className="mx-4" /> : null}
                        <li className="flex items-center gap-3 pr-2">
                          <Link
                            href={`/search?q=${encodeURIComponent(x.query)}${x.kinds ? `&kind=${encodeURIComponent(x.kinds.split(",")[0] ?? "")}` : ""}${x.source ? `&source=${encodeURIComponent(x.source)}` : ""}`}
                            className="flex min-h-12 min-w-0 flex-1 items-center gap-3 px-4 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                          >
                            <Bookmark aria-hidden className="size-4 shrink-0 text-fg-muted" />
                            <span className="truncate type-body text-fg">{x.name}</span>
                          </Link>
                          <IconButton
                            size="sm"
                            variant="ghost"
                            label={`Remove saved search ${x.name}`}
                            icon={<X />}
                            onClick={() => removeSaved.mutate(x.id, { onError: (e) => toast.error(errorMessage(e)) })}
                          />
                        </li>
                      </Fragment>
                    ))}
                  </ul>
                </Card>
              </Section>
            ) : null}
            {recent.length ? (
          <Section
            title="Recent"
            action={
              <Button size="sm" variant="ghost" onClick={clear}>
                Clear
              </Button>
            }
          >
            <Card padding="none">
              <ul>
                {recent.map((r, i) => (
                  <Fragment key={r}>
                    {i > 0 ? <Divider className="mx-4" /> : null}
                    <li className="flex items-center gap-3 pr-2">
                      <Link href={`/search?q=${encodeURIComponent(r)}`} className="flex min-h-12 min-w-0 flex-1 items-center gap-3 px-4 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
                        <Clock aria-hidden className="size-4 shrink-0 text-fg-muted" />
                        <span className="truncate type-body text-fg">{r}</span>
                      </Link>
                      <IconButton size="sm" variant="ghost" label={`Remove ${r}`} icon={<X />} onClick={() => forget(r)} />
                    </li>
                  </Fragment>
                ))}
              </ul>
            </Card>
          </Section>
            ) : null}
          </div>
        ) : (
          <EmptyState icon={<SearchIcon />} title="Type to search" info="Narrow with filters: from:@name, in:#channel, type:page, after:2026-11-01, during:week." />
        )
      ) : !searchable ? (
        <p className="type-body text-fg-muted">Type 2 or more characters, or a filter like from:@name.</p>
      ) : res.isPending ? (
        <Card padding="none">
          <SkeletonGroup label="Searching" className="flex flex-col">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </SkeletonGroup>
        </Card>
      ) : res.isError && !res.data ? (
        <ErrorState title="Couldn't search" info={errorMessage(res.error)} onRetry={() => void res.refetch()} retrying={res.isFetching} />
      ) : (
        <div className="flex flex-col gap-section">
          {!isSaved ? (
            <Button
              variant="ghost"
              size="sm"
              className="self-start"
              leftIcon={<BookmarkPlus />}
              loading={save.isPending}
              onClick={() =>
                save.mutate(
                  { name: q, query: q, ...(kind ? { kinds: kind } : {}), ...(source !== "all" ? { source } : {}) },
                  { onSuccess: () => toast.success("Search saved."), onError: (e) => toast.error(errorMessage(e, "Couldn't save the search.")) },
                )
              }
            >
              Save this search
            </Button>
          ) : null}
          {skipped.length > 0 ? <Banner tone="info">Not searched: {skipped.join(", ")}.</Banner> : null}
          {unanswered.length > 0 ? (
            <Banner
              tone="warning"
              action={
                <Button size="sm" variant="secondary" loading={res.isFetching} onClick={() => void res.refetch()}>
                  Retry
                </Button>
              }
            >
              Some results may be missing: {unanswered.join(", ")} didn&apos;t answer.
            </Banner>
          ) : null}
          {groups.length === 0 ? (
            <EmptyState
              icon={<SearchX />}
              title={`No results for “${q}”`}
              action={
                source !== "all" ? (
                  <Button size="sm" variant="secondary" onClick={() => setSourceParam(null)}>
                    Search every source
                  </Button>
                ) : kind ? (
                  <Button size="sm" variant="secondary" onClick={() => setKind(null)}>
                    Search everything
                  </Button>
                ) : scope && !everywhere ? (
                  <Button size="sm" variant="secondary" onClick={() => setScopeParam("all")}>
                    Search {scope.allLabel.charAt(0).toLowerCase() + scope.allLabel.slice(1)}
                  </Button>
                ) : undefined
              }
            />
          ) : (
            groups.map((g) => (
              <Section
                key={g.feature}
                title={`${g.label} · ${g.items.length}`}
                action={
                  !kind && g.total > g.items.length ? (
                    <Button size="sm" variant="ghost" onClick={() => setKind(g.feature)}>
                      See all
                    </Button>
                  ) : undefined
                }
              >
                <Card padding="none">
                  <ul aria-label={g.label}>
                    {g.items.map((item, i) => (
                      <Fragment key={`${item.kind}:${item.id}`}>
                        {i > 0 ? <Divider className="mx-4" /> : null}
                        <li>
                          <ResultLink item={item} orgId={orgId} query={q} />
                        </li>
                      </Fragment>
                    ))}
                  </ul>
                </Card>
              </Section>
            ))
          )}
        </div>
      )}
    </div>
  );
}

function ResultLink({ item, orgId, query }: { item: SearchResult; orgId?: string | undefined; query: string }) {
  const Icon = searchIcon(item.icon, item.kind);
  const where0 = searchResultContext(item, orgId);
  // The badge already names a connected hit's source and account.
  const where = item.source && where0 === sourceLabel(item.source, "") ? "" : where0;
  return (
    <Link href={item.link} className="flex min-h-14 items-center gap-3 px-4 py-3 hover:bg-sunken focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring">
      <Icon aria-hidden className="size-5 shrink-0 text-fg-muted" />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <Highlighted className="line-clamp-2 type-body font-semibold text-fg" text={item.title} query={query} />
        {where || item.snippet ? (
          <span className="line-clamp-2 type-label text-fg-muted">
            {where}
            {where && item.snippet ? " · " : ""}
            {item.snippet ? <Highlighted text={item.snippet} query={query} /> : null}
          </span>
        ) : null}
        {item.source ? <SourceBadge source={item.source} showAccount className="mt-1 self-start" /> : null}
      </span>
      <span className="shrink-0 type-caption text-fg-subtle">{formatRelative(item.at)}</span>
    </Link>
  );
}
