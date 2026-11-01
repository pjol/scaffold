import {
  errorMessage,
  groupSearchResults,
  pushRecentSearch,
  SEARCH_MIN_QUERY_CHARS,
  searchResultContext,
  unansweredSearchProviders,
  useDebouncedValue,
  useSearch,
  useSearchScope,
  type SearchResult,
} from "@scaffoldapp/shared";
import { Banner, Button, EmptyState, ErrorState, IconButton, Input, ListGroup, ListRow, Screen, SkeletonRow, Text, ToggleChip, usePressScale, useTheme } from "@scaffoldapp/ui-native";
import { router, type Href } from "expo-router";
import { Clock, Search, SearchX, X } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Pressable, View } from "react-native";
import Animated from "react-native-reanimated";
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

/**
 * Search (Home → Search): everything the person can see across features, grouped by kind.
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
  const searchable = q.length >= SEARCH_MIN_QUERY_CHARS;
  const scope = useSearchScope();
  const [everywhere, setEverywhere] = useState(false);
  const orgId = scope && !everywhere ? scope.id : undefined;
  const res = useSearch({ q, limit: 40, orgId }, { enabled: searchable });
  const groups = res.data && searchable ? groupSearchResults(res.data.items) : [];
  const unanswered = res.data ? unansweredSearchProviders(res.data.providers) : [];

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
      </View>

      {text.trim().length < SEARCH_MIN_QUERY_CHARS ? (
        recent.length ? (
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
        ) : text.trim() ? (
          <Text tone="muted">Type {SEARCH_MIN_QUERY_CHARS} or more characters.</Text>
        ) : (
          <EmptyState icon={Search} title="Type to search" />
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
          {unanswered.length ? (
            <Banner tone="warning" title={`Some results may be missing: ${unanswered.join(", ")} didn't answer.`} action={{ label: "Retry", onPress: () => void res.refetch() }} />
          ) : null}
          {groups.length === 0 ? (
            <EmptyState
              icon={SearchX}
              title={`No results for “${q}”`}
              action={
                scope && !everywhere ? (
                  <Button variant="secondary" onPress={() => setEverywhere(true)}>
                    {`Search ${scope.allLabel.charAt(0).toLowerCase() + scope.allLabel.slice(1)}`}
                  </Button>
                ) : undefined
              }
            />
          ) : (
            groups.map((g) => (
              <ListGroup key={g.kind} title={g.label}>
                {g.items.map((item) => (
                  <ResultRow key={`${item.kind}:${item.id}`} item={item} where={searchResultContext(item, orgId)} onPress={() => open(item)} />
                ))}
              </ListGroup>
            ))
          )}
        </View>
      )}
    </Screen>
  );
}

function ResultRow({ item, where, onPress }: { item: SearchResult; where: string; onPress: () => void }) {
  const theme = useTheme();
  const s = theme.space;
  const press = usePressScale(0.99);
  const Icon = searchIcon(item.icon, item.kind);
  const sub = [where, item.snippet].filter(Boolean).join(" · ");
  return (
    <Pressable onPress={onPress} onPressIn={press.onPressIn} onPressOut={press.onPressOut} accessibilityRole="link" accessibilityLabel={[item.title, where, item.snippet].filter(Boolean).join(". ")}>
      <Animated.View style={[{ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.lg, paddingVertical: s[2], paddingHorizontal: s[4] }, press.style]}>
        <Icon size={20} color={theme.colors.textMuted} />
        <View style={{ flex: 1, gap: s[1] }}>
          <Text weight={600} numberOfLines={2}>
            {item.title}
          </Text>
          {sub ? (
            <Text role="caption" tone="muted" numberOfLines={2}>
              {sub}
            </Text>
          ) : null}
        </View>
        <Text role="caption" tone="subtle">
          {ago(item.at)}
        </Text>
      </Animated.View>
    </Pressable>
  );
}
