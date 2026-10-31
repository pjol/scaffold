import { errorMessage, formatDay, isNotFound, todayKey, useDeletePlace, usePlaceView, WEEKDAYS, type Place } from "@scaffoldapp/shared";
import { Banner, Body, Button, Caption, ConfirmDialog, EmptyState, ErrorState, ListGroup, ListRow, PhotoGrid, Screen, Skeleton, Title, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import * as WebBrowser from "expo-web-browser";
import { Globe, MapPinOff, Navigation, Pencil, Phone, Trash2 } from "lucide-react-native";
import { useState } from "react";
import { Linking, Platform, View } from "react-native";
import { FavoriteButton, OpenBadge, PlaceStatusBadge, useCategoryLabel } from "./parts";

async function openDirections(p: Place) {
  const ll = `${p.lat},${p.lng}`;
  const url = Platform.OS === "ios" ? `http://maps.apple.com/?daddr=${ll}&q=${encodeURIComponent(p.name)}` : `geo:${ll}?q=${ll}(${encodeURIComponent(p.name)})`;
  try {
    await Linking.openURL(url);
  } catch {
    await WebBrowser.openBrowserAsync(`https://www.openstreetmap.org/directions?to=${encodeURIComponent(ll)}`);
  }
}

function Body_({ place, owned }: { place: Place; owned: boolean }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const label = useCategoryLabel();
  const remove = useDeletePlace();
  const [confirm, setConfirm] = useState(false);
  const today = todayKey(place.hours);
  const icon = (I: typeof Phone) => <I size={22} color={c.textMuted} strokeWidth={2} />;

  return (
    <>
      {place.photos.length > 0 ? <PhotoGrid photos={place.photos.map((p) => ({ id: p.id, url: p.url }))} columns={3} /> : null}
      <View style={{ gap: s[2] }}>
        <Title>{place.name}</Title>
        <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: s[2] }}>
          <Caption tone="muted">{label(place.category)}</Caption>
          <OpenBadge hours={place.hours} />
          {place.status !== "approved" ? <PlaceStatusBadge status={place.status} /> : null}
        </View>
        {place.status === "rejected" && place.review_note ? <Banner tone="danger" title={place.review_note} /> : null}
      </View>
      {place.description ? <Body>{place.description}</Body> : null}
      <ListGroup title="Contact">
        <ListRow title={place.address || "Directions"} leading={icon(Navigation)} onPress={() => void openDirections(place)} accessibilityHint="Opens directions in your maps app" />
        {place.phone ? <ListRow title={place.phone} leading={icon(Phone)} onPress={() => void Linking.openURL(`tel:${place.phone.replace(/[^\d+]/g, "")}`)} /> : null}
        {place.website ? (
          <ListRow title={place.website.replace(/^https?:\/\//, "").replace(/\/$/, "")} leading={icon(Globe)} onPress={() => void WebBrowser.openBrowserAsync(place.website)} />
        ) : null}
      </ListGroup>
      {place.hours ? (
        <ListGroup title="Hours" info={place.hours.note || (place.hours.timezone ? `Times are in ${place.hours.timezone.replace(/_/g, " ")}.` : undefined)}>
          {WEEKDAYS.map((d) => (
            <ListRow
              key={d.key}
              title={d.label}
              trailing={
                <Body tabular weight={d.key === today ? 600 : 400}>
                  {formatDay(place.hours?.weekly[d.key] ?? [])}
                </Body>
              }
            />
          ))}
        </ListGroup>
      ) : null}
      {owned ? (
        <View style={{ gap: s[2] }}>
          <Button variant="secondary" leftIcon={Pencil} onPress={() => router.push({ pathname: "/places/[id]/edit", params: { id: place.id } })}>
            Edit place
          </Button>
          <Button variant="ghost" leftIcon={<Trash2 size={20} color={c.dangerStrong} />} onPress={() => setConfirm(true)}>
            Delete place
          </Button>
          <ConfirmDialog
            open={confirm}
            onClose={() => setConfirm(false)}
            title={`Delete ${place.name}?`}
            confirmLabel="Delete place"
            tone="danger"
            onConfirm={async () => {
              await remove.mutateAsync(place.id);
              toast.success("Place deleted");
              router.back();
            }}
            errorText={(e) => errorMessage(e, "Couldn't delete it. Try again.")}
          >
            <Body>Its photos go too. This can't be undone.</Body>
          </ConfirmDialog>
        </View>
      ) : null}
    </>
  );
}

/** A place: approved, or the caller's own in any status. */
export function PlaceDetailScreen({ id }: { id: string }) {
  const theme = useTheme();
  const s = theme.space;
  const view = usePlaceView(id);
  const place = view.data?.place;
  return (
    <Screen edges={["bottom"]} gap={s[6]}>
      <Stack.Screen options={{ title: place?.name ?? "Place", headerRight: place?.status === "approved" ? () => <FavoriteButton placeId={id} /> : undefined }} />
      {view.isPending ? (
        <View style={{ gap: s[4] }} accessibilityLabel="Loading" accessibilityRole="progressbar">
          <Skeleton height={160} />
          <Skeleton shape="text" width="70%" height={theme.type.title.lineHeight} />
          <Skeleton height={120} />
        </View>
      ) : view.isError ? (
        isNotFound(view.error) ? (
          <EmptyState icon={MapPinOff} title="This place isn't available" action={<Button variant="secondary" onPress={() => router.back()}>Go back</Button>} />
        ) : (
          <ErrorState title="Couldn't load this place" info={errorMessage(view.error)} onRetry={() => void view.refetch()} retrying={view.isRefetching} />
        )
      ) : (
        <Body_ place={view.data.place} owned={view.data.owned} />
      )}
    </Screen>
  );
}
