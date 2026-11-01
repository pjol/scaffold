import { errorMessage, useCalendarFeed, useDeleteCalendarFeed, useRotateCalendarFeed } from "@scaffoldapp/shared";
import { Banner, Button, Card, ConfirmDialog, ErrorState, ListGroup, ListRow, Screen, Skeleton, Text, haptic, toast, useTheme } from "@scaffoldapp/ui-native";
import { CalendarSync, Share2 } from "lucide-react-native";
import { useState } from "react";
import { RefreshControl, Share, View } from "react-native";

const dateFormat = new Intl.DateTimeFormat("en-US", { dateStyle: "medium" });

/** Settings → Calendar feed: a secret URL calendar apps subscribe to (events you host or RSVP'd to). */
export function CalendarFeedScreen() {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const feed = useCalendarFeed();
  const rotate = useRotateCalendarFeed();
  const remove = useDeleteCalendarFeed();
  const [url, setUrl] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<"replace" | "off" | null>(null);

  const create = async () => {
    const r = await rotate.mutateAsync();
    haptic.success();
    setUrl(r.url);
  };
  const share = async () => {
    if (!url) return;
    try {
      await Share.share({ message: url });
    } catch {
      toast.error("Couldn't open sharing. Try again.");
    }
  };

  return (
    <Screen edges={["bottom"]} gap={s[6]} refreshControl={<RefreshControl refreshing={feed.isRefetching} onRefresh={() => void feed.refetch()} />}>
      {feed.isPending ? (
        <View style={{ gap: s[3] }} accessibilityLabel="Loading calendar feed" accessibilityRole="progressbar">
          <Skeleton height={theme.tap.lg} />
          <Skeleton height={theme.tap.base} />
        </View>
      ) : feed.isError ? (
        <ErrorState title="Couldn't load your calendar feed" info={errorMessage(feed.error)} onRetry={() => void feed.refetch()} retrying={feed.isRefetching} />
      ) : (
        <>
          <ListGroup
            title="Calendar feed"
            info="Subscribe to this link in Google Calendar, Apple Calendar or Outlook to see events you host or RSVP'd to. Anyone with the link can read those events, so keep it to yourself."
          >
            <ListRow
              title={feed.data.active && feed.data.created_at ? `Active since ${dateFormat.format(new Date(feed.data.created_at))}` : "No feed link yet"}
              leading={<CalendarSync size={22} color={c.textMuted} strokeWidth={2} />}
            />
          </ListGroup>
          {url ? (
            <View style={{ gap: s[3] }}>
              <Banner tone="info" title="Copy this link now. It's shown once; replace it to get a new one." />
              <Card variant="sunken">
                <Text role="label" selectable>
                  {url}
                </Text>
              </Card>
              <Button leftIcon={Share2} onPress={() => void share()}>
                Share link
              </Button>
            </View>
          ) : null}
          <View style={{ gap: s[2] }}>
            {feed.data.active ? (
              <>
                <Button variant="secondary" onPress={() => setConfirm("replace")}>
                  Replace link
                </Button>
                <Button variant="ghost" onPress={() => setConfirm("off")}>
                  Turn off
                </Button>
              </>
            ) : (
              <Button loading={rotate.isPending} onPress={() => void create().catch((e: unknown) => toast.error(errorMessage(e, "Couldn't create the link. Try again.")))}>
                Create feed link
              </Button>
            )}
          </View>
        </>
      )}
      <ConfirmDialog
        open={confirm === "replace"}
        onClose={() => setConfirm(null)}
        title="Replace your calendar link?"
        confirmLabel="Replace link"
        onConfirm={create}
        errorText={(e) => errorMessage(e, "Couldn't replace the link. Try again.")}
      >
        <Text role="body">The old link stops working. Calendars subscribed to it stop updating.</Text>
      </ConfirmDialog>
      <ConfirmDialog
        open={confirm === "off"}
        onClose={() => setConfirm(null)}
        title="Turn off your calendar feed?"
        tone="danger"
        confirmLabel="Turn off"
        onConfirm={async () => {
          await remove.mutateAsync();
          setUrl(null);
          toast.success("Calendar feed turned off");
        }}
        errorText={(e) => errorMessage(e, "Couldn't turn off the feed. Try again.")}
      >
        <Text role="body">The link stops working. Calendars subscribed to it stop updating.</Text>
      </ConfirmDialog>
    </Screen>
  );
}
