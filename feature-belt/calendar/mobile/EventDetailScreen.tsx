import {
  calendarEndpoints,
  describeCalendarRecurrence,
  errorMessage,
  formatCalendarRange,
  isApiError,
  useCalendarEvent,
  useDeleteCalendarEvent,
  useEndpoints,
  useRespondCalendarEvent,
  type CalendarScope,
} from "@scaffoldapp/shared";
import { Avatar, Badge, Body, Button, ConfirmDialog, EmptyState, ErrorState, ListGroup, ListRow, Screen, SegmentedControl, Sheet, Skeleton, Text, Title, toast, useTheme } from "@scaffoldapp/ui-native";
import { File, Paths } from "expo-file-system";
import { router, Stack } from "expo-router";
import * as Sharing from "expo-sharing";
import { CalendarClock, CalendarPlus, CalendarX, MapPin, Pencil, Repeat, Trash2, UserRound, Video } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { Linking, RefreshControl, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { CalendarDot, RESPONSE_LABEL, RESPONSE_TONE, calendarColors, useCalendarZone } from "./parts";

function Row({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  const theme = useTheme();
  return (
    <View style={{ flexDirection: "row", gap: theme.space[3], alignItems: "flex-start", minHeight: theme.size.control }}>
      <View style={{ paddingTop: theme.space[0.5] }}>{icon}</View>
      <View style={{ flex: 1 }}>{children}</View>
    </View>
  );
}

/** One event: time, place, guests and replies; RSVP as a guest; edit, share as .ics or delete as the organizer. */
export function EventDetailScreen({ id, occurrence }: { id: string; occurrence: string | null }) {
  const theme = useTheme();
  const s = theme.space;
  const c = theme.colors;
  const { zone } = useCalendarZone();
  const event = useCalendarEvent(id, occurrence);
  const respond = useRespondCalendarEvent();
  const remove = useDeleteCalendarEvent();
  const api = useEndpoints(calendarEndpoints);
  const [confirm, setConfirm] = useState(false);
  const [scopeOpen, setScopeOpen] = useState(false);

  if (event.isPending) {
    return (
      <Screen edges={["bottom"]} gap={s[4]}>
        <Skeleton shape="text" width="70%" height={theme.type.title.lineHeight} />
        <Skeleton shape="text" width="50%" height={theme.type.body.lineHeight} />
        <Skeleton height={theme.tap.lg * 2} />
      </Screen>
    );
  }
  if (event.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        {isApiError(event.error) && event.error.status === 404 ? (
          <EmptyState icon={CalendarX} title="This event isn't on your calendar" />
        ) : (
          <ErrorState title="Couldn't load this event" info={errorMessage(event.error)} onRetry={() => void event.refetch()} retrying={event.isRefetching} />
        )}
      </Screen>
    );
  }
  const e = event.data;
  const owner = e.role === "owner";
  const col = calendarColors(theme, e.color);

  const del = async (scope: CalendarScope) => {
    await remove.mutateAsync({ id: e.event_id, scope, occurrence: e.occurrence_start });
    toast.success(e.booking_id ? "Booking cancelled" : "Event deleted");
    router.back();
  };

  const shareIcs = async () => {
    try {
      const text = await api.eventIcs(e.event_id);
      const file = new File(Paths.cache, `${e.title.toLowerCase().replace(/[^a-z0-9]+/g, "-").slice(0, 60) || "event"}.ics`);
      if (file.exists) file.delete();
      file.create();
      file.write(text);
      await Sharing.shareAsync(file.uri, { mimeType: "text/calendar", UTI: "com.apple.ical.ics" });
    } catch (err) {
      toast.error(errorMessage(err, "Couldn't share the event."));
    }
  };

  const edit = () => router.push({ pathname: "/calendar/form", params: { id: e.event_id, ...(e.occurrence_start ? { occurrence: e.occurrence_start } : {}) } });

  return (
    <Screen edges={["bottom"]} gap={s[6]} refreshControl={<RefreshControl refreshing={event.isRefetching} onRefresh={() => void event.refetch()} />}>
      <Stack.Screen
        options={{
          headerRight: () => (
            <ResourceMenu
              feature="calendar"
              kind="calendar.event"
              id={e.event_id}
              title={e.title}
              actions={[
                ...(owner ? [{ id: "edit", label: "Edit", icon: Pencil, onSelect: edit }] : []),
                { id: "ics", label: "Add to another calendar", icon: CalendarPlus, onSelect: () => void shareIcs() },
                ...(owner ? [{ id: "delete", label: e.booking_id ? "Cancel booking" : "Delete", icon: Trash2, destructive: true, onSelect: () => (e.recurring ? setScopeOpen(true) : setConfirm(true)) }] : []),
              ]}
            />
          ),
        }}
      />
      <View style={{ flexDirection: "row", gap: s[3], alignItems: "flex-start" }}>
        <View style={{ paddingTop: s[3] }}>
          <CalendarDot color={col.dot} />
        </View>
        <Title style={{ flex: 1 }}>{e.title}</Title>
      </View>
      <View style={{ gap: s[3] }}>
        <Row icon={<CalendarClock size={20} color={c.textMuted} />}>
          <Body>{formatCalendarRange(e, zone)}</Body>
          {!e.all_day && e.timezone !== zone ? (
            <Text role="label" tone="muted">
              {formatCalendarRange(e, e.timezone)} in {e.timezone.replace(/_/g, " ")}
            </Text>
          ) : null}
        </Row>
        {e.recurrence ? (
          <Row icon={<Repeat size={20} color={c.textMuted} />}>
            <Body>{describeCalendarRecurrence(e.recurrence)}</Body>
          </Row>
        ) : null}
        {e.location ? (
          <Row icon={<MapPin size={20} color={c.textMuted} />}>
            <Body selectable>{e.location}</Body>
          </Row>
        ) : null}
        {e.conference_url ? (
          <Row icon={<Video size={20} color={c.textMuted} />}>
            <Button size="md" variant="primary" leftIcon={Video} onPress={() => void Linking.openURL(e.conference_url ?? "")} style={{ alignSelf: "flex-start" }}>
              Join video call
            </Button>
          </Row>
        ) : null}
        <Row icon={<UserRound size={20} color={c.textMuted} />}>
          <Body>{owner ? "You're the organizer" : `Organized by ${e.owner.display_name || "a deleted account"}`}</Body>
        </Row>
      </View>
      {e.description ? <Body selectable>{e.description}</Body> : null}

      {e.role === "attendee" ? (
        <View style={{ gap: s[2] }}>
          <Text role="label" weight={600}>
            Going?{e.recurring ? " (the whole series)" : ""}
          </Text>
          <SegmentedControl<"accepted" | "tentative" | "declined">
            accessibilityLabel="Your reply"
            {...(e.my_response && e.my_response !== "needs_action" ? { value: e.my_response } : {})}
            onValueChange={(v) =>
              respond.mutate({ id: e.event_id, response: v }, { onSuccess: () => toast.success("Reply sent"), onError: (err) => toast.error(errorMessage(err, "Couldn't send your reply.")) })
            }
            options={[
              { value: "accepted", label: "Yes" },
              { value: "tentative", label: "Maybe" },
              { value: "declined", label: "No" },
            ]}
          />
        </View>
      ) : owner ? (
        <Button variant="secondary" leftIcon={Pencil} onPress={edit}>
          Edit
        </Button>
      ) : null}

      {e.attendees.length > 0 ? (
        <ListGroup title={`${e.attendees.length} ${e.attendees.length === 1 ? "guest" : "guests"}`}>
          {e.attendees.map((a) => (
            <ListRow
              key={a.id}
              title={a.name || a.email}
              leading={<Avatar name={a.name || a.email} src={a.avatar_url} size="sm" />}
              trailing={<Badge tone={RESPONSE_TONE[a.response]}>{RESPONSE_LABEL[a.response]}</Badge>}
              chevron={false}
            />
          ))}
        </ListGroup>
      ) : null}

      <ConfirmDialog
        open={confirm}
        onClose={() => setConfirm(false)}
        title={e.booking_id ? "Cancel this booking?" : `Delete “${e.title}”?`}
        tone="danger"
        confirmLabel={e.booking_id ? "Cancel booking" : "Delete"}
        onConfirm={() => del("all")}
      >
        <Body>{e.booking_id ? "The person who booked gets a cancellation email." : e.attendees.length > 0 ? "Guests are told it's cancelled." : "It can't be undone."}</Body>
      </ConfirmDialog>
      <Sheet open={scopeOpen} onClose={() => setScopeOpen(false)} title="Delete recurring event">
        <ListGroup>
          {(
            [
              ["this", "This event"],
              ["following", "This and following events"],
              ["all", "All events"],
            ] as const
          ).map(([scope, label]) => (
            <ListRow
              key={scope}
              title={label}
              destructive
              chevron={false}
              onPress={() => {
                setScopeOpen(false);
                del(scope).catch((err: unknown) => toast.error(errorMessage(err, "Couldn't delete the event.")));
              }}
            />
          ))}
        </ListGroup>
      </Sheet>
    </Screen>
  );
}
