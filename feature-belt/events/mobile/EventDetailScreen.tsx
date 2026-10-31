import {
  errorMessage,
  formatEventRange,
  isApiError,
  useCancelEvent,
  useClearRsvp,
  useDisableInviteLink,
  useEvent,
  useEventIcs,
  useEventTopic,
  useMe,
  useRemoveCoHost,
  useRotateInviteLink,
  useRsvp,
  type EventItem,
  type RsvpChoice,
} from "@scaffoldapp/shared";
import {
  Avatar,
  Badge,
  Banner,
  Body,
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  IconButton,
  InfoTip,
  ListGroup,
  ListRow,
  Screen,
  Sheet,
  Skeleton,
  Text,
  Title,
  TitleSm,
  haptic,
  toast,
  useTheme,
} from "@scaffoldapp/ui-native";
import { File, Paths } from "expo-file-system";
import { Image } from "expo-image";
import { router, Stack } from "expo-router";
import * as Sharing from "expo-sharing";
import { Ban, CalendarClock, CalendarPlus, CalendarX, Check, Ellipsis, Globe, Link2, Lock, MapPin, Pencil, RefreshCw, Share2, Unlink, Users, UsersRound, Video } from "lucide-react-native";
import { useRef, useState, type ReactNode } from "react";
import { Linking as RNLinking, RefreshControl, Share, View } from "react-native";
import { goingText, hasEnded, RSVP_LABEL, VISIBILITY_INFO, VISIBILITY_LABEL } from "./parts";
import { shareableLink } from "./links";

const CHOICES: { value: RsvpChoice; label: string }[] = [
  { value: "going", label: "Going" },
  { value: "maybe", label: "Maybe" },
  { value: "declined", label: "Can't go" },
];

function icsFileName(title: string): string {
  const slug = title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 60);
  return `${slug || "event"}.ics`;
}

/** The link that joins an invite-only event with a code (web URL when configured, else the app link). */
function inviteLink(code: string): string {
  return shareableLink("/events/join", { code });
}

function InfoRow({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  const theme = useTheme();
  return (
    <View style={{ flexDirection: "row", gap: theme.space[3], alignItems: "flex-start", minHeight: theme.size.control }}>
      <View style={{ paddingTop: theme.space[0.5] }}>{icon}</View>
      <View style={{ flex: 1 }}>{children}</View>
    </View>
  );
}

function RsvpCard({ event }: { event: EventItem }) {
  const theme = useTheme();
  const s = theme.space;
  const rsvp = useRsvp();
  const clear = useClearRsvp();
  const mine = event.my_rsvp?.status ?? null;
  const pending = rsvp.isPending ? rsvp.variables?.status : null;

  const choose = (status: RsvpChoice) => {
    if (status === mine) return;
    rsvp.mutate(
      { id: event.id, status },
      {
        onSuccess: (e) => {
          haptic.success();
          if (e.my_rsvp?.status === "waitlisted" && status === "going") toast.success(`It's full. You're number ${e.my_rsvp.waitlist_position ?? 1} on the waitlist.`);
          else toast.success(status === "going" ? "You're going" : status === "maybe" ? "Marked as maybe" : "Marked as can't go");
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't save your RSVP. Try again.")),
      },
    );
  };

  return (
    <Card>
      <View style={{ gap: s[4] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <TitleSm>Are you going?</TitleSm>
          {event.capacity ? <InfoTip content={`${event.capacity} spots. When it's full, Going puts you on the waitlist and you move up automatically when a spot opens.`} /> : null}
        </View>
        {mine === "waitlisted" ? (
          <Banner
            tone="warning"
            title={`You're number ${event.my_rsvp?.waitlist_position ?? 1} on the waitlist.`}
            action={{ label: "Leave waitlist", onPress: () => clear.mutate(event.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't leave the waitlist. Try again.")) }) }}
          />
        ) : null}
        <View accessibilityRole="radiogroup" accessibilityLabel="Your RSVP" style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
          {CHOICES.map((c) => {
            const selected = mine === c.value || (c.value === "going" && mine === "waitlisted");
            return (
              <View key={c.value} style={{ flexGrow: 1, flexBasis: "30%" }}>
                <Button
                  accessibilityRole="radio"
                  accessibilityState={{ checked: selected }}
                  variant={selected ? "primary" : "secondary"}
                  fullWidth
                  leftIcon={selected ? Check : undefined}
                  loading={pending === c.value}
                  disabled={rsvp.isPending || clear.isPending}
                  onPress={() => choose(c.value)}
                >
                  {c.label}
                </Button>
              </View>
            );
          })}
        </View>
        {mine && mine !== "waitlisted" ? (
          <Button
            variant="ghost"
            loading={clear.isPending}
            onPress={() => clear.mutate(event.id, { onSuccess: () => toast.success("RSVP removed"), onError: (e) => toast.error(errorMessage(e, "Couldn't remove your RSVP. Try again.")) })}
          >
            Remove my RSVP
          </Button>
        ) : null}
      </View>
    </Card>
  );
}

type HostAction = "cancel" | null;

/** One event: time and place, RSVP, who's coming, hosts, calendar export and host actions. */
export function EventDetailScreen({ id }: { id: string }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const me = useMe();
  const event = useEvent(id);
  useEventTopic(id);
  const ics = useEventIcs();
  const cancel = useCancelEvent();
  const rotate = useRotateInviteLink();
  const disable = useDisableInviteLink();
  const removeCoHost = useRemoveCoHost();
  const [menu, setMenu] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const next = useRef<HostAction>(null);

  if (event.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <View style={{ gap: s[4] }} accessibilityLabel="Loading event" accessibilityRole="progressbar">
          <Skeleton height={180} />
          <Skeleton shape="text" width="70%" height={theme.type.title.lineHeight} />
          <Skeleton shape="text" width="50%" height={theme.type.body.lineHeight} />
          <Skeleton height={120} />
        </View>
      </Screen>
    );
  }
  if (event.isError) {
    const gone = isApiError(event.error) && event.error.status === 404;
    return (
      <Screen edges={["bottom"]} centered>
        {gone ? (
          <EmptyState
            icon={CalendarX}
            title="This event isn't available"
            action={
              <Button variant="secondary" onPress={() => (router.canGoBack() ? router.back() : router.replace("/events"))}>
                All events
              </Button>
            }
          />
        ) : (
          <ErrorState title="Couldn't load this event" info={errorMessage(event.error)} onRetry={() => void event.refetch()} retrying={event.isRefetching} />
        )}
      </Screen>
    );
  }

  const e = event.data;
  const ended = hasEnded(e);
  const cancelled = e.status === "cancelled";
  const live = !cancelled && !ended;
  const creator = e.host_role === "creator";
  const seesAttendees = !!e.host_role || e.my_rsvp?.status === "going" || e.my_rsvp?.status === "maybe" || e.my_rsvp?.status === "waitlisted";
  const iconProps = { size: 20, color: c.textMuted, strokeWidth: 2 } as const;

  const addToCalendar = async () => {
    try {
      const text = await ics.mutateAsync(e.id);
      const file = new File(Paths.cache, icsFileName(e.title));
      if (file.exists) file.delete();
      file.create();
      file.write(text);
      if (!(await Sharing.isAvailableAsync())) {
        toast.error("Sharing isn't available on this device.");
        return;
      }
      await Sharing.shareAsync(file.uri, { mimeType: "text/calendar", UTI: "com.apple.ical.ics", dialogTitle: "Add to calendar" });
    } catch (err) {
      toast.error(errorMessage(err, "Couldn't create the calendar file. Try again."));
    }
  };

  const shareInvite = async (code: string) => {
    try {
      await Share.share({ message: `${e.title}: ${inviteLink(code)}` });
    } catch {
      toast.error("Couldn't open sharing. Try again.");
    }
  };

  const newInvite = () =>
    rotate.mutate(e.id, {
      onSuccess: (link) => void shareInvite(link.code),
      onError: (err) => toast.error(errorMessage(err, "Couldn't make an invite link. Try again.")),
    });

  const hostRows: { key: string; title: string; icon: typeof Pencil; destructive?: boolean; onPress: () => void }[] = [];
  if (live) hostRows.push({ key: "edit", title: "Edit", icon: Pencil, onPress: () => router.push({ pathname: "/events/form", params: { id: e.id } }) });
  if (live && e.invite_code) hostRows.push({ key: "share", title: "Share invite link", icon: Share2, onPress: () => void shareInvite(e.invite_code ?? "") });
  if (live) hostRows.push({ key: "rotate", title: e.invite_code ? "New invite link" : "Create invite link", icon: e.invite_code ? RefreshCw : Link2, onPress: newInvite });
  if (live && e.invite_code)
    hostRows.push({
      key: "disable",
      title: "Turn off invite link",
      icon: Unlink,
      onPress: () => disable.mutate(e.id, { onSuccess: () => toast.success("Invite link turned off"), onError: (err) => toast.error(errorMessage(err, "Couldn't turn off the link. Try again.")) }),
    });
  if (creator && live)
    hostRows.push({
      key: "cancel",
      title: "Cancel event",
      icon: Ban,
      destructive: true,
      onPress: () => {
        next.current = "cancel";
      },
    });

  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={event.isRefetching} onRefresh={() => void event.refetch()} />}>
      {e.host_role && hostRows.length > 0 ? (
        <Stack.Screen options={{ headerRight: () => <IconButton label="Event actions" icon={Ellipsis} variant="ghost" onPress={() => setMenu(true)} /> }} />
      ) : null}

      <View style={{ gap: s[4] }}>
        {e.cover_url ? (
          <Image source={{ uri: e.cover_url }} style={{ width: "100%", aspectRatio: 16 / 9, borderRadius: theme.radius.lg, backgroundColor: c.sunken }} contentFit="cover" accessibilityIgnoresInvertColors />
        ) : null}
        <Title>{e.title}</Title>
        {cancelled ? <Banner tone="danger" title="This event was cancelled." /> : ended ? <Banner tone="info" title="This event has ended." /> : null}
        <View style={{ gap: s[2] }}>
          <InfoRow icon={<CalendarClock {...iconProps} />}>
            <Body>{formatEventRange(e.starts_at, e.ends_at, e.timezone)}</Body>
          </InfoRow>
          {e.location_text ? (
            <InfoRow icon={<MapPin {...iconProps} />}>
              <Body selectable>{e.location_text}</Body>
            </InfoRow>
          ) : null}
          {e.online_url ? (
            <InfoRow icon={<Video {...iconProps} />}>
              <Text role="body" tone="primary" accessibilityRole="link" onPress={() => void RNLinking.openURL(e.online_url ?? "")} style={{ textDecorationLine: "underline" }}>
                {e.online_url}
              </Text>
            </InfoRow>
          ) : null}
          <InfoRow icon={e.visibility === "invite" ? <Lock {...iconProps} /> : e.visibility === "public" ? <Globe {...iconProps} /> : <UsersRound {...iconProps} />}>
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
              <Body>{VISIBILITY_LABEL[e.visibility]}</Body>
              <InfoTip content={VISIBILITY_INFO[e.visibility]} />
            </View>
          </InfoRow>
        </View>
        <Button variant="secondary" leftIcon={CalendarPlus} loading={ics.isPending} onPress={() => void addToCalendar()}>
          Add to calendar
        </Button>
      </View>

      {e.details ? <Body selectable>{e.details}</Body> : null}

      {live ? <RsvpCard event={e} /> : null}

      <ListGroup title="Who's coming" {...(seesAttendees ? {} : { info: "Only hosts and people who RSVP'd see who's coming." })}>
        <ListRow
          title={goingText(e)}
          leading={<Users {...iconProps} />}
          trailing={
            <View style={{ flexDirection: "row", gap: s[1.5] }}>
              {e.waitlist_count > 0 ? (
                <Badge size="sm" tone="warning">
                  {`${e.waitlist_count} waiting`}
                </Badge>
              ) : null}
              {e.is_full && !cancelled ? (
                <Badge size="sm" tone="warning">
                  Full
                </Badge>
              ) : null}
            </View>
          }
          {...(seesAttendees ? { chevron: true, onPress: () => router.push({ pathname: "/events/[id]/attendees", params: { id: e.id } }) } : {})}
        />
        {e.maybe_count > 0 ? <ListRow title={`${e.maybe_count} ${RSVP_LABEL.maybe.toLowerCase()}`} leading={<Users {...iconProps} />} /> : null}
      </ListGroup>

      <ListGroup title="Hosts">
        <ListRow title={e.host.display_name || "Deleted user"} leading={<Avatar name={e.host.display_name || "?"} src={e.host.avatar_url} size="md" />} trailing={<Badge size="sm">Creator</Badge>} />
        {e.co_hosts.map((h) => {
          const self = h.id === me.data?.id;
          return (
            <ListRow
              key={h.id}
              title={h.display_name || "Unnamed"}
              leading={<Avatar name={h.display_name || "?"} src={h.avatar_url} size="md" />}
              trailing={
                creator || self ? (
                  <Button
                    variant="ghost"
                    size="md"
                    loading={removeCoHost.isPending && removeCoHost.variables?.userId === h.id}
                    onPress={() =>
                      removeCoHost.mutate(
                        { id: e.id, userId: h.id },
                        {
                          onSuccess: () => toast.success(self ? "You're no longer a co-host" : `${h.display_name || "They"} removed as co-host`),
                          onError: (err) => toast.error(errorMessage(err, "Couldn't remove the co-host. Try again.")),
                        },
                      )
                    }
                  >
                    {self ? "Step down" : "Remove"}
                  </Button>
                ) : (
                  <Badge size="sm">Co-host</Badge>
                )
              }
            />
          );
        })}
      </ListGroup>

      <Sheet
        open={menu}
        onClose={() => setMenu(false)}
        title="Event actions"
        onDismissed={() => {
          if (next.current === "cancel") setConfirmCancel(true);
          next.current = null;
        }}
      >
        <ListGroup>
          {hostRows.map((r) => {
            const Icon = r.icon;
            return (
              <ListRow
                key={r.key}
                title={r.title}
                leading={<Icon size={20} color={r.destructive ? c.dangerStrong : c.textMuted} strokeWidth={2} />}
                {...(r.destructive ? { destructive: true } : {})}
                onPress={() => {
                  r.onPress();
                  setMenu(false);
                }}
              />
            );
          })}
        </ListGroup>
      </Sheet>

      <ConfirmDialog
        open={confirmCancel}
        onClose={() => setConfirmCancel(false)}
        title="Cancel this event?"
        tone="danger"
        confirmLabel="Cancel event"
        cancelLabel="Keep event"
        onConfirm={async () => {
          await cancel.mutateAsync(e.id);
          haptic.success();
          toast.success("Event cancelled. Everyone who RSVP'd was told.");
        }}
        errorText={(err) => errorMessage(err, "Couldn't cancel the event. Try again.")}
      >
        <Body>Everyone who RSVP'd gets a notice. This can't be undone.</Body>
      </ConfirmDialog>
    </Screen>
  );
}
