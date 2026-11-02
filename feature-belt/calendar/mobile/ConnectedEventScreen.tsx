import {
  calendarJoinLabel,
  calendarProviderPlatform,
  calendarProviderProduct,
  errorMessage,
  formatCalendarRange,
  isApiError,
  sourceProductLabel,
  useCalendarConnectedEvent,
  useDeleteCalendarConnectedEvent,
  useRespondCalendarConnectedEvent,
  type CalendarConnectedRef,
} from "@scaffoldapp/shared";
import { Avatar, Badge, Body, Button, ConfirmDialog, EmptyState, ErrorState, ListGroup, ListRow, ProviderIcon, Screen, SegmentedControl, Skeleton, SourceBadge, Text, Title, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { CalendarClock, CalendarX, ExternalLink, MapPin, Repeat, Trash2, UserRound, Video } from "lucide-react-native";
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

/**
 * An event from a connected calendar (Google, Outlook), live: time, calendar, the meeting
 * link (Meet, Teams and Zoom open in their own apps), guests, RSVP, and delete for the
 * organizer — written back to the platform. Editing happens on the web or on the platform.
 */
export function ConnectedEventScreen({ eventRef }: { eventRef: CalendarConnectedRef }) {
  const theme = useTheme();
  const s = theme.space;
  const c = theme.colors;
  const { zone } = useCalendarZone();
  const event = useCalendarConnectedEvent(eventRef);
  const respond = useRespondCalendarConnectedEvent();
  const remove = useDeleteCalendarConnectedEvent();
  const [confirm, setConfirm] = useState(false);

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
    const fix = isApiError(event.error) ? (event.error.details?.["fix_url"] as string | undefined) : undefined;
    return (
      <Screen edges={["bottom"]} centered gap={s[3]}>
        {isApiError(event.error) && event.error.status === 404 ? (
          <EmptyState icon={CalendarX} title="This event isn't there any more" />
        ) : (
          <ErrorState title="Couldn't load the event" info={errorMessage(event.error)} onRetry={() => void event.refetch()} retrying={event.isRefetching} />
        )}
        {fix ? (
          <Button variant="secondary" onPress={() => router.push(fix.slice(1) as never)}>
            Reconnect
          </Button>
        ) : null}
      </Screen>
    );
  }
  const e = event.data;
  const product = sourceProductLabel(e.source.provider, e.source.product);
  const col = calendarColors(theme, e.color);
  const platform = calendarProviderPlatform(e.conference_provider);
  const otherZone = !e.all_day && e.timezone && e.timezone !== zone;

  return (
    <Screen edges={["bottom"]} gap={s[6]} refreshControl={<RefreshControl refreshing={event.isRefetching} onRefresh={() => void event.refetch()} />}>
      <Stack.Screen
        options={{
          headerRight: () => (
            <ResourceMenu
              feature={`connectors:${e.connection_id}`}
              title={`${product} · ${e.source.account}`}
              actions={[
                ...(e.web_url ? [{ id: "open", label: `Open in ${product}`, icon: ExternalLink, onSelect: () => void Linking.openURL(e.web_url) }] : []),
                ...(e.can_edit ? [{ id: "delete", label: "Delete", icon: Trash2, destructive: true, onSelect: () => setConfirm(true) }] : []),
              ]}
            />
          ),
        }}
      />
      <View style={{ gap: s[2] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <CalendarDot color={col.dot} />
          <Title style={{ flex: 1 }}>{e.title}</Title>
        </View>
        <SourceBadge source={e.source} showAccount style={{ alignSelf: "flex-start" }} />
      </View>
      <View style={{ gap: s[3] }}>
        <Row icon={<CalendarClock size={20} color={c.textMuted} />}>
          <Body>{formatCalendarRange(e, zone)}</Body>
          {otherZone ? (
            <Text role="label" tone="muted">
              {formatCalendarRange(e, e.timezone)} in {e.timezone.replace(/_/g, " ")}
            </Text>
          ) : null}
        </Row>
        {e.recurring ? (
          <Row icon={<Repeat size={20} color={c.textMuted} />}>
            <Body>Part of a repeating event</Body>
          </Row>
        ) : null}
        {e.location && e.location !== e.conference_url ? (
          <Row icon={<MapPin size={20} color={c.textMuted} />}>
            <Body selectable>{e.location}</Body>
          </Row>
        ) : null}
        {e.conference_url ? (
          <Row icon={platform ? <ProviderIcon provider={platform} product={calendarProviderProduct(e.conference_provider)} size={20} /> : <Video size={20} color={c.textMuted} />}>
            <Button size="md" variant="primary" leftIcon={ExternalLink} onPress={() => void Linking.openURL(e.conference_url ?? "")} style={{ alignSelf: "flex-start" }}>
              {calendarJoinLabel(e.conference_provider)}
            </Button>
          </Row>
        ) : null}
        <Row icon={<UserRound size={20} color={c.textMuted} />}>
          <Body>{e.organizer.self ? `You organize it (${e.source.account})` : `Organized by ${e.organizer.name || e.organizer.email || "someone else"}`}</Body>
        </Row>
      </View>
      {e.description ? <Body selectable>{e.description}</Body> : null}

      {e.my_response ? (
        e.can_respond ? (
          <View style={{ gap: s[2] }}>
            <Text role="label" weight={600}>
              Going?
            </Text>
            <SegmentedControl<"accepted" | "tentative" | "declined">
              accessibilityLabel="Your reply"
              {...(e.my_response !== "needs_action" ? { value: e.my_response } : {})}
              onValueChange={(v) =>
                respond.mutate(
                  { ref: eventRef, response: v },
                  { onSuccess: () => toast.success(`Reply sent through ${product}`), onError: (err) => toast.error(errorMessage(err, "Couldn't send your reply.")) },
                )
              }
              options={[
                { value: "accepted", label: "Yes" },
                { value: "tentative", label: "Maybe" },
                { value: "declined", label: "No" },
              ]}
            />
          </View>
        ) : (
          <Text role="label" tone="muted">
            Reply in {product}, or allow changes to {product} in Settings → Connections.
          </Text>
        )
      ) : null}

      {e.attendees.length > 0 ? (
        <ListGroup title={`${e.attendees.length} ${e.attendees.length === 1 ? "guest" : "guests"}`}>
          {e.attendees.map((a) => (
            <ListRow
              key={a.email}
              title={a.self ? `${a.name || a.email} (you)` : a.name || a.email}
              leading={<Avatar name={a.name || a.email} size="sm" />}
              trailing={<Badge tone={RESPONSE_TONE[a.response]}>{RESPONSE_LABEL[a.response]}</Badge>}
              chevron={false}
            />
          ))}
        </ListGroup>
      ) : null}

      <ConfirmDialog
        open={confirm}
        onClose={() => setConfirm(false)}
        title={`Delete “${e.title}”?`}
        tone="danger"
        confirmLabel="Delete"
        onConfirm={async () => {
          try {
            await remove.mutateAsync({ ref: eventRef });
            toast.success(`Deleted from ${product}`);
            router.back();
          } catch (err) {
            toast.error(errorMessage(err, "Couldn't delete the event."));
          }
        }}
      >
        <Body>{e.attendees.length > 0 ? `It's deleted from ${product}, which tells the guests.` : `It's deleted from ${product}.`}</Body>
      </ConfirmDialog>
    </Screen>
  );
}
