import { errorMessage, useEvent, useEventAttendees, useEventTopic, type RsvpStatus } from "@scaffoldapp/shared";
import { Avatar, Badge, Button, EmptyState, ErrorState, ListGroup, ListRow, Screen, SkeletonRow, Spinner, ToggleChip, useTheme } from "@scaffoldapp/ui-native";
import { Users } from "lucide-react-native";
import { useState } from "react";
import { RefreshControl, View } from "react-native";
import { RSVP_LABEL } from "./parts";

/** Who's coming, by RSVP status. Hosts also see who can't go. */
export function AttendeesScreen({ id }: { id: string }) {
  const theme = useTheme();
  const s = theme.space;
  const event = useEvent(id);
  useEventTopic(id);
  const [status, setStatus] = useState<RsvpStatus>("going");
  const list = useEventAttendees(id, status);
  const rows = list.data?.items ?? [];
  const e = event.data;

  const tabs: { value: RsvpStatus; label: string }[] = [
    { value: "going", label: e ? `Going ${e.going_count}` : "Going" },
    { value: "maybe", label: e ? `Maybe ${e.maybe_count}` : "Maybe" },
    ...(e && (e.capacity || e.waitlist_count > 0) ? [{ value: "waitlisted" as const, label: `Waitlist ${e.waitlist_count}` }] : []),
    ...(e?.host_role ? [{ value: "declined" as const, label: "Can't go" }] : []),
  ];

  return (
    <Screen edges={["bottom"]} gap={s[6]} refreshControl={<RefreshControl refreshing={list.isRefetching && !list.isFetchingNextPage} onRefresh={() => void list.refetch()} />}>
      <View accessibilityRole="tablist" style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
        {tabs.map((t) => (
          <ToggleChip key={t.value} pressed={status === t.value} onPressedChange={() => setStatus(t.value)}>
            {t.label}
          </ToggleChip>
        ))}
      </View>
      <ListGroup>
        {list.isPending ? (
          <View accessibilityLabel="Loading people" accessibilityRole="progressbar">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </View>
        ) : list.isError ? (
          <ErrorState compact title="Couldn't load this list" info={errorMessage(list.error)} onRetry={() => void list.refetch()} retrying={list.isRefetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={Users} title={status === "waitlisted" ? "No one waiting" : `No one marked ${RSVP_LABEL[status].toLowerCase()}`} />
        ) : (
          rows.map((a) => (
            <ListRow
              key={a.user.id}
              title={a.user.display_name || "Unnamed"}
              leading={<Avatar name={a.user.display_name || "?"} src={a.user.avatar_url} size="md" />}
              trailing={a.waitlist_position ? <Badge size="sm">{`#${a.waitlist_position}`}</Badge> : undefined}
            />
          ))
        )}
        {list.hasNextPage ? (
          list.isFetchingNextPage ? (
            <Spinner />
          ) : (
            <Button variant="ghost" onPress={() => void list.fetchNextPage()}>
              Load more
            </Button>
          )
        ) : null}
      </ListGroup>
    </Screen>
  );
}
