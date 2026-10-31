import { errorMessage, useEvents } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, IconButton, Screen, SegmentedControl, Skeleton, Spinner, ToggleChip, enterUp, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { CalendarDays, Plus, SearchX } from "lucide-react-native";
import { useState } from "react";
import { RefreshControl, View } from "react-native";
import Animated from "react-native-reanimated";
import { EventCard } from "./parts";

type When = "upcoming" | "past";
type Filter = "all" | "hosting" | "attending";

const FILTERS: { value: Filter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "hosting", label: "Hosting" },
  { value: "attending", label: "Going" },
];

function emptyTitle(when: When, filter: Filter): string {
  if (filter === "hosting") return when === "upcoming" ? "You're not hosting anything coming up" : "You haven't hosted an event yet";
  if (filter === "attending") return when === "upcoming" ? "Nothing on your calendar" : "No past events you went to";
  return when === "upcoming" ? "No upcoming events" : "No past events";
}

/** Events: upcoming or past, filtered to all, hosting or going; pull to refresh. */
export function EventsScreen() {
  const theme = useTheme();
  const s = theme.space;
  const [when, setWhen] = useState<When>("upcoming");
  const [filter, setFilter] = useState<Filter>("all");
  const events = useEvents({ when, filter, limit: 20 });
  const rows = events.data?.items ?? [];

  return (
    <Screen
      edges={["bottom"]}
      gap={s[6]}
      refreshControl={<RefreshControl refreshing={events.isRefetching && !events.isFetchingNextPage} onRefresh={() => void events.refetch()} />}
    >
      <Stack.Screen
        options={{
          headerRight: () => <IconButton label="New event" icon={Plus} variant="ghost" onPress={() => router.push("/events/form")} />,
        }}
      />
      <View style={{ gap: s[3] }}>
        <SegmentedControl<When>
          accessibilityLabel="When"
          options={[
            { value: "upcoming", label: "Upcoming" },
            { value: "past", label: "Past" },
          ]}
          value={when}
          onValueChange={setWhen}
        />
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
          {FILTERS.map((f) => (
            <ToggleChip key={f.value} pressed={filter === f.value} onPressedChange={() => setFilter(f.value)}>
              {f.label}
            </ToggleChip>
          ))}
        </View>
      </View>

      {events.isPending ? (
        <View style={{ gap: s[4] }} accessibilityLabel="Loading events" accessibilityRole="progressbar">
          {[0, 1, 2].map((i) => (
            <View key={i} style={{ flexDirection: "row", gap: s[4] }}>
              <Skeleton width={theme.tap.lg} height={theme.tap.lg} />
              <View style={{ flex: 1, gap: s[2] }}>
                <Skeleton shape="text" width="70%" height={theme.type.titleSm.lineHeight} />
                <Skeleton shape="text" width="50%" height={theme.type.label.lineHeight} />
              </View>
            </View>
          ))}
        </View>
      ) : events.isError ? (
        <ErrorState title="Couldn't load events" info={errorMessage(events.error)} onRetry={() => void events.refetch()} retrying={events.isRefetching} />
      ) : rows.length === 0 ? (
        <EmptyState
          icon={filter === "all" ? CalendarDays : SearchX}
          title={emptyTitle(when, filter)}
          action={
            when === "upcoming" ? (
              <Button variant="secondary" leftIcon={Plus} onPress={() => router.push("/events/form")}>
                Create an event
              </Button>
            ) : undefined
          }
        />
      ) : (
        <View style={{ gap: s[4] }}>
          {rows.map((e, i) => (
            <Animated.View key={e.id} entering={enterUp(theme, Math.min(i, 6))}>
              <EventCard event={e} onPress={() => router.push({ pathname: "/events/[id]", params: { id: e.id } })} />
            </Animated.View>
          ))}
          {events.hasNextPage ? (
            events.isFetchingNextPage ? (
              <Spinner />
            ) : (
              <Button variant="ghost" onPress={() => void events.fetchNextPage()}>
                Load more
              </Button>
            )
          ) : null}
        </View>
      )}
    </Screen>
  );
}
