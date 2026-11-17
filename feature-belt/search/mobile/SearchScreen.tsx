import {
  errorMessage,
  groupSearchResultsByFeature,
  parseSearchOperators,
  pushRecentSearch,
  removeSearchOperator,
  SEARCH_OPERATOR_HINTS,
  searchHighlight,
  searchOperatorLabel,
  searchProviderLabel,
  searchQueryReady,
  searchResultContext,
  searchSourceOptions,
  skippedSearchProviders,
  sourceLabel,
  unansweredSearchProviders,
  useDebouncedValue,
  useRemoveSavedSearch,
  useSavedSearches,
  useSaveSearch,
  useSearch,
  useSearchScope,
  type SearchResult,
} from "@scaffoldapp/shared";
import { Banner, Button, Chip, EmptyState, ErrorState, IconButton, Input, ListGroup, ListRow, Screen, SkeletonRow, SourceBadge, SourceFilter, Text, ToggleChip, toast, usePressScale, useTheme } from "@scaffoldapp/ui-native";
import { router, type Href } from "expo-router";
import { Bookmark, BookmarkPlus, Clock, Filter, Search, SearchX, X } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Pressable, View } from "react-native";
import Animated from "react-native-reanimated";
import { appName } from "@/lib/version";
import { userStorage } from "@/lib/storage";
import { searchIcon } from "./icons";

const RECENT_KEY = "search.recent";

/** "now", "5 min", "3 h", "2 d", then the date. */
function ago(iso: string): string {
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return "now";
  if (s < 3600) return `${Math.floor(s / 60)} min`;
  if (s < 86400) return `${Math.floor(s / 3600)} h`;
  if (s < 7 * 86400) return `${Math.floor(s / 86400)} d`;
  return new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

/** Text with the query's words marked. */
function Marked({ text, query }: { text: string; query: string }) {
  const theme = useTheme();
  return (
    <>
      {searchHighlight(text, query).map((p, i) =>
        p.match ? (
          <Text key={i} style={{ backgroundColor: theme.colors.primarySoft, color: theme.colors.text }}>
            {p.text}
          </Text>
        ) : (
          p.text
        ),
      )}
    </>
  );
}

/**
 * Search (Home → Search): everything the person can see across features, grouped by feature
 * with "See all", operator chips (from:, in:, type:, dates, is:) and highlighted words; saved
 * searches (every device) and the filters you can type when empty.
 * With an org scope registered, the current org by default and a chip for every org; rows
 * say where each hit lives. Recent searches live in per-user storage (wiped on sign-out).
 * A result opens its app path.
 */
export function SearchScreen() {
  const theme = useTheme();
  const s = theme.space;
  const [text, setText] = useState("");
  const [recent, setRecent] = useState<string[]>([]);
  const q = useDebouncedValue(text, 250).trim();
  const searchable = searchQueryReady(q);
  const [kind, setKind] = useState("");
  const saved = useSavedSearches();
  const save = useSaveSearch();
  const removeSaved = useRemoveSavedSearch();
  const savedList = saved.data?.items ?? [];
  const chips = parseSearchOperators(text).chips;
  const scope = useSearchScope();
  const [everywhere, setEverywhere] = useState(false);
  const orgId = scope && !everywhere ? scope.id : undefined;
  const [source, setSource] = useState("all");
  const res = useSearch({ q, limit: 40, orgId, ...(kind ? { kinds: [kind] } : {}), ...(source !== "all" ? { source } : {}) }, { enabled: searchable });
  const sourceOptions = searchSourceOptions(res.data?.sources, appName());
  const groups = res.data && searchable ? groupSearchResultsByFeature(res.data.items, res.data.providers) : [];
  const unanswered = res.data ? unansweredSearchProviders(res.data.providers) : [];
  const skipped = res.data ? skippedSearchProviders(res.data.providers) : [];
  const names: Record<string, string[]> = {};
  for (const f of res.data?.filters?.from ?? []) names[f.value] = f.names;
  const isSaved = savedList.some((x) => x.query === q && x.kinds === kind);

  useEffect(() => {
    void userStorage.get<string[]>(RECENT_KEY).then((v) => setRecent(Array.isArray(v) ? v : []));
  }, []);
  const saveRecent = (next: string[]) => {
    setRecent(next);
    void userStorage.set(RECENT_KEY, next);
  };
  const open = (item: SearchResult) => {
    saveRecent(pushRecentSearch(recent, q));
    router.push(item.link as Href);
  };

  return (
    <Screen edges={["bottom"]} gap={s[6]} scrollProps={{ keyboardShouldPersistTaps: "handled", keyboardDismissMode: "on-drag" }}>
      <View style={{ gap: s[3] }}>
        <Input
          value={text}
          onChangeText={setText}
          placeholder="Search"
          accessibilityLabel="Search"
          leadingIcon={Search}
          autoFocus
          autoCorrect={false}
          returnKeyType="search"
          maxLength={200}
          trailing={text ? <IconButton label="Clear search" icon={X} variant="ghost" onPress={() => setText("")} /> : undefined}
        />
        {scope ? (
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
            <ToggleChip pressed={!everywhere} onPressedChange={() => setEverywhere(false)}>
              {scope.name}
            </ToggleChip>
            <ToggleChip pressed={everywhere} onPressedChange={() => setEverywhere(true)}>
              {scope.allLabel}
            </ToggleChip>
          </View>
        ) : null}
        {chips.length ? (
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }} accessibilityLabel="Filters in your search">
            {chips.map((c) => (
              <Chip key={c.token} size="sm" icon={X} accessibilityLabel={`Remove ${searchOperatorLabel(c, names)}`} onPress={() => setText(removeSearchOperator(text, c))}>
                {searchOperatorLabel(c, names)}
              </Chip>
            ))}
          </View>
        ) : null}
        {kind ? (
          <View style={{ flexDirection: "row", gap: s[2] }}>
            <Chip size="sm" selected icon={X} accessibilityLabel={`Show every feature, not only ${searchProviderLabel(kind)}`} onPress={() => setKind("")}>
              {searchProviderLabel(kind)}
            </Chip>
          </View>
        ) : null}
        {searchable ? <SourceFilter options={sourceOptions} value={source} onChange={setSource} style={{ marginHorizontal: -s[4] }} /> : null}
      </View>

      {!searchQueryReady(text.trim()) ? (
        text.trim() ? (
          <Text tone="muted">Type 2 or more characters, or a filter like from:@name.</Text>
        ) : (
          <View style={{ gap: s[6] }}>
            {savedList.length ? (
              <ListGroup title="Saved searches">
                {savedList.map((x) => (
                  <ListRow
                    key={x.id}
                    title={x.name}
                    leading={<Bookmark size={20} color={theme.colors.textMuted} />}
                    chevron={false}
                    onPress={() => {
                      setKind(x.kinds.split(",")[0] ?? "");
                      setText(x.query);
                    }}
                    trailing={<IconButton label={`Remove saved search ${x.name}`} icon={X} variant="ghost" onPress={() => removeSaved.mutate(x.id, { onError: (e) => toast.error(errorMessage(e)) })} />}
                  />
                ))}
              </ListGroup>
            ) : null}
            {recent.length ? (
              <View style={{ gap: s[2] }}>
                <ListGroup title="Recent">
                  {recent.map((r) => (
                    <ListRow
                      key={r}
                      title={r}
                      leading={<Clock size={20} color={theme.colors.textMuted} />}
                      chevron={false}
                      onPress={() => setText(r)}
                      trailing={<IconButton label={`Remove ${r}`} icon={X} variant="ghost" onPress={() => saveRecent(recent.filter((x) => x !== r))} />}
                    />
                  ))}
                </ListGroup>
                <View style={{ alignItems: "flex-start" }}>
                  <Button variant="ghost" onPress={() => saveRecent([])}>
                    Clear recent
                  </Button>
                </View>
              </View>
            ) : null}
            <ListGroup title="Filters">
              {SEARCH_OPERATOR_HINTS.map((h) => (
                <ListRow key={h.example} title={`${h.example}  ${h.label}`} leading={<Filter size={20} color={theme.colors.textMuted} />} chevron={false} onPress={() => setText(h.example)} />
              ))}
            </ListGroup>
          </View>
        )
      ) : !searchable || res.isPending ? (
        <View accessibilityLabel="Searching" accessibilityRole="progressbar" style={{ gap: s[2] }}>
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </View>
      ) : res.isError && !res.data ? (
        <ErrorState title="Couldn't search" info={errorMessage(res.error)} onRetry={() => void res.refetch()} retrying={res.isFetching} />
      ) : (
        <View style={{ gap: s[6] }}>
          {!isSaved ? (
            <View style={{ alignItems: "flex-start" }}>
              <Button
                variant="ghost"
                leftIcon={BookmarkPlus}
                loading={save.isPending}
                onPress={() =>
                  save.mutate(
                    { name: q, query: q, ...(kind ? { kinds: kind } : {}) },
                    { onSuccess: () => toast.success("Search saved."), onError: (e) => toast.error(errorMessage(e, "Couldn't save the search.")) },
                  )
                }
              >
                Save this search
              </Button>
            </View>
          ) : null}
          {skipped.length ? <Banner tone="info" title={`Not searched: ${skipped.join(", ")}.`} /> : null}
          {unanswered.length ? (
            <Banner tone="warning" title={`Some results may be missing: ${unanswered.join(", ")} didn't answer.`} action={{ label: "Retry", onPress: () => void res.refetch() }} />
          ) : null}
          {groups.length === 0 ? (
            <EmptyState
              icon={SearchX}
              title={`No results for “${q}”`}
              action={
                source !== "all" ? (
                  <Button variant="secondary" onPress={() => setSource("all")}>
                    Search every source
                  </Button>
                ) : scope && !everywhere ? (
                  <Button variant="secondary" onPress={() => setEverywhere(true)}>
                    {`Search ${scope.allLabel.charAt(0).toLowerCase() + scope.allLabel.slice(1)}`}
                  </Button>
                ) : undefined
              }
            />
          ) : (
            groups.map((g) => (
              <View key={g.feature} style={{ gap: s[2] }}>
                <ListGroup title={g.label}>
                  {(kind ? g.items : g.items.slice(0, 5)).map((item) => (
                    <ResultRow key={`${item.kind}:${item.id}`} item={item} query={q} where={searchResultContext(item, orgId)} onPress={() => open(item)} />
                  ))}
                </ListGroup>
                {!kind && g.total > 5 ? (
                  <View style={{ alignItems: "flex-start" }}>
                    <Button variant="ghost" onPress={() => setKind(g.feature)}>
                      {`See all in ${g.label}`}
                    </Button>
                  </View>
                ) : null}
              </View>
            ))
          )}
        </View>
      )}
    </Screen>
  );
}

function ResultRow({ item, where, query, onPress }: { item: SearchResult; where: string; query: string; onPress: () => void }) {
  const theme = useTheme();
  const s = theme.space;
  const press = usePressScale(0.99);
  const Icon = searchIcon(item.icon, item.kind);
  const sub = [where, item.snippet].filter(Boolean).join(" · ");
  const lead = where && item.snippet ? `${where} · ` : where;
  return (
    <Pressable onPress={onPress} onPressIn={press.onPressIn} onPressOut={press.onPressOut} accessibilityRole="link" accessibilityLabel={[item.title, where, item.snippet, item.source?.type === "connected" ? sourceLabel(item.source, "") : ""].filter(Boolean).join(". ")}>
      <Animated.View style={[{ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.lg, paddingVertical: s[2], paddingHorizontal: s[4] }, press.style]}>
        <Icon size={20} color={theme.colors.textMuted} />
        <View style={{ flex: 1, gap: s[1] }}>
          <Text weight={600} numberOfLines={2}>
            <Marked text={item.title} query={query} />
          </Text>
          {sub ? (
            <Text role="caption" tone="muted" numberOfLines={2}>
              {lead}
              {item.snippet ? <Marked text={item.snippet} query={query} /> : null}
            </Text>
          ) : null}
          <SourceBadge source={item.source} />
        </View>
        <Text role="caption" tone="subtle">
          {ago(item.at)}
        </Text>
      </Animated.View>
    </Pressable>
  );
}
