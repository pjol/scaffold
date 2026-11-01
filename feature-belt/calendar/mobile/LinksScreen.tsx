import { errorMessage, formatCalendarDuration, useCalendarLinks, useUpdateCalendarLink, type CalendarBookingLink } from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, Screen, Skeleton, Spinner, Switch, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Link2, Lock, Share2 } from "lucide-react-native";
import { Linking, RefreshControl, Share, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { CalendarDot, calendarColors } from "./parts";

function share(link: CalendarBookingLink) {
  // The system share sheet offers Copy as well as apps.
  Share.share({ message: link.url, url: link.url, title: link.title }).catch(() => toast.error("Couldn't open the share sheet."));
}

function LinkCard({ link }: { link: CalendarBookingLink }) {
  const theme = useTheme();
  const s = theme.space;
  const update = useUpdateCalendarLink();
  const setActive = (active: boolean) =>
    update.mutate({ id: link.id, body: { active } }, { onSuccess: () => toast.success(active ? "Link is on" : "Link paused"), onError: (e) => toast.error(errorMessage(e, "Couldn't change the link.")) });
  return (
    <Card>
      <View style={{ gap: s[3] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <CalendarDot color={calendarColors(theme, link.color).dot} />
          <Text role="titleSm" style={{ flex: 1 }} numberOfLines={2}>
            {link.title}
          </Text>
          <ResourceMenu
            feature="calendar"
            kind="calendar.booking_link"
            id={link.id}
            title={link.title}
            actions={[
              { id: "share", label: "Share link", icon: Share2, onSelect: () => share(link) },
              { id: "open", label: "Open booking page", icon: Link2, onSelect: () => void Linking.openURL(link.url) },
            ]}
          />
        </View>
        <Text role="label" tone="muted" numberOfLines={1}>
          {link.durations.map(formatCalendarDuration).join(", ")} · /book/{link.slug}
        </Text>
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
          {link.audience === "internal" ? (
            <Badge tone="info" icon={Lock}>
              {link.org_name ? `${link.org_name} only` : "Members only"}
            </Badge>
          ) : (
            <Badge tone="neutral">Anyone</Badge>
          )}
          {link.upcoming_count > 0 ? <Badge tone="primary">{`${link.upcoming_count} upcoming`}</Badge> : null}
        </View>
        <Switch value={link.active} onValueChange={setActive} label="Accept bookings" />
        <Button variant="secondary" leftIcon={Share2} onPress={() => share(link)}>
          Share link
        </Button>
      </View>
    </Card>
  );
}

/** Booking links: share or pause them (create and edit on the web). */
export function LinksScreen() {
  const theme = useTheme();
  const s = theme.space;
  const links = useCalendarLinks();
  const rows = links.data?.items ?? [];
  return (
    <Screen edges={["bottom"]} gap={s[4]} refreshControl={<RefreshControl refreshing={links.isRefetching && !links.isFetchingNextPage} onRefresh={() => void links.refetch()} />}>
      {links.isPending ? (
        <View style={{ gap: s[4] }} accessibilityLabel="Loading booking links" accessibilityRole="progressbar">
          <Skeleton height={theme.tap.xl * 2} />
          <Skeleton height={theme.tap.xl * 2} />
        </View>
      ) : links.isError ? (
        <ErrorState title="Couldn't load booking links" info={errorMessage(links.error)} onRetry={() => void links.refetch()} retrying={links.isRefetching} />
      ) : rows.length === 0 ? (
        <EmptyState icon={Link2} title="No booking links yet" info="Create booking links in the web app; they show up here to share." />
      ) : (
        <>
          {rows.map((l) => (
            <LinkCard key={l.id} link={l} />
          ))}
          {links.hasNextPage ? (
            links.isFetchingNextPage ? (
              <Spinner />
            ) : (
              <Button variant="ghost" onPress={() => void links.fetchNextPage()}>
                Load more
              </Button>
            )
          ) : null}
        </>
      )}
    </Screen>
  );
}
