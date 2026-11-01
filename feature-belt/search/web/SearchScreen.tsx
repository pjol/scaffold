"use client";

import {
  errorMessage,
  groupSearchResults,
  SEARCH_MIN_QUERY_CHARS,
  searchKindLabel,
  searchResultContext,
  unansweredSearchProviders,
  useDebouncedValue,
  useMe,
  useSearch,
  useSearchScope,
  type SearchResult,
} from "@scaffoldapp/shared";
import { Banner, Button, Card, Divider, EmptyState, ErrorState, IconButton, Input, SkeletonGroup, SkeletonRow, ToggleChip } from "@scaffoldapp/ui-web";
import { Clock, SearchX, X } from "lucide-react";
import Link from "next/link";
import { Fragment, useEffect, useMemo, useState } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { APP_NAME } from "@/lib/app";
import { formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { SearchIcon, searchIcon } from "./icons";
import { useRecentSearches } from "./recent";

/**
 * /search?q=&kind=&scope=all: every result for a query, grouped by kind, with a filter per
 * kind found. With an org scope registered, the current org by default and every org with
 * `scope=all`. The query and filters live in the URL (shareable, back works). Partial
 * answers say which feature didn't respond. Empty query: recent searches from this browser.
 */
export function SearchScreen() {
  const [urlQ, setUrlQ] = useUrlState("q", "");
  const [kind, setKind] = useUrlState("kind", "");
  const [scopeParam, setScopeParam] = useUrlState("scope", "");
  const scope = useSearchScope();
  const everywhere = scopeParam === "all";
  const orgId = scope && !everywhere ? scope.id : undefined;
  const [text, setText] = useState(urlQ);
  const debounced = useDebouncedValue(text, 250);
  const me = useMe();
  const { recent, remember, forget, clear } = useRecentSearches(me.data?.id);

  useEffect(() => setText(urlQ), [urlQ]);
  useEffect(() => {
    const next = debounced.trim();
    if (next !== urlQ) setUrlQ(next || null);
  }, [debounced]);

  const q = urlQ.trim();
  const searchable = q.length >= SEARCH_MIN_QUERY_CHARS;
  const all = useSearch({ q, limit: 50, orgId }, { enabled: searchable });
  const filtered = useSearch({ q, kinds: kind ? [kind] : [], limit: 50, orgId }, { enabled: searchable && !!kind });
  const res = kind ? filtered : all;
  const groups = useMemo(() => (res.data ? groupSearchResults(res.data.items) : []), [res.data]);
  const kinds = useMemo(() => {
    const ks = all.data ? groupSearchResults(all.data.items).map((g) => g.kind) : [];
    return kind && !ks.includes(kind) ? [...ks, kind] : ks;
  }, [all.data, kind]);
  const unanswered = res.data ? unansweredSearchProviders(res.data.providers) : [];

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
        {searchable && kinds.length > 1 ? (
          <div className="flex flex-wrap gap-2" role="group" aria-label="Filter by kind">
            <ToggleChip pressed={!kind} onPressedChange={() => setKind(null)}>
              All
            </ToggleChip>
            {kinds.map((k) => (
              <ToggleChip key={k} pressed={kind === k} onPressedChange={(on) => setKind(on ? k : null)}>
                {searchKindLabel(k)}
              </ToggleChip>
            ))}
          </div>
        ) : null}
      </div>

      {!q ? (
        recent.length ? (
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
        ) : (
          <EmptyState icon={<SearchIcon />} title="Type to search" />
        )
      ) : !searchable ? (
        <p className="type-body text-fg-muted">Type {SEARCH_MIN_QUERY_CHARS} or more characters.</p>
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
                kind ? (
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
              <Section key={g.kind} title={`${g.label} · ${g.items.length}`}>
                <Card padding="none">
                  <ul aria-label={g.label}>
                    {g.items.map((item, i) => (
                      <Fragment key={`${item.kind}:${item.id}`}>
                        {i > 0 ? <Divider className="mx-4" /> : null}
                        <li>
                          <ResultLink item={item} orgId={orgId} />
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

function ResultLink({ item, orgId }: { item: SearchResult; orgId?: string }) {
  const Icon = searchIcon(item.icon, item.kind);
  const sub = [searchResultContext(item, orgId), item.snippet].filter(Boolean).join(" · ");
  return (
    <Link href={item.link} className="flex min-h-14 items-center gap-3 px-4 py-3 hover:bg-sunken focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring">
      <Icon aria-hidden className="size-5 shrink-0 text-fg-muted" />
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="line-clamp-2 type-body font-semibold text-fg">{item.title}</span>
        {sub ? <span className="line-clamp-2 type-label text-fg-muted">{sub}</span> : null}
      </span>
      <span className="shrink-0 type-caption text-fg-subtle">{formatRelative(item.at)}</span>
    </Link>
  );
}
