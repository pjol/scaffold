import { errorMessage, flattenPages, useFavoritePlaces, useMyPlaces, type Place } from "@scaffoldapp/shared";
import { Body, Button, Caption, EmptyState, ErrorState, SegmentedControl, SkeletonRow, Spinner, useLayout, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { Heart, MapPinPlus, Plus } from "lucide-react-native";
import { useMemo, useState } from "react";
import { FlatList, Pressable, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { PlaceRow, PlaceStatusBadge, PlaceThumb, useCategoryLabel } from "./parts";

type Tab = "saved" | "mine";

function MineRow({ place }: { place: Place }) {
  const theme = useTheme();
  const s = theme.space;
  const label = useCategoryLabel();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={place.name}
      onPress={() => router.push({ pathname: "/places/[id]", params: { id: place.id } })}
      style={{ flexDirection: "row", alignItems: "center", gap: s[3], paddingVertical: s[2], paddingHorizontal: s[2], minHeight: theme.tap.lg }}
    >
      <PlaceThumb url={place.photos[0]?.url ?? null} />
      <View style={{ flex: 1, gap: s[0.5] }}>
        <Body weight={600} numberOfLines={1}>
          {place.name}
        </Body>
        <Caption tone="muted" numberOfLines={2}>
          {place.status === "rejected" && place.review_note ? place.review_note : label(place.category)}
        </Caption>
      </View>
      <PlaceStatusBadge status={place.status} />
    </Pressable>
  );
}

const loading = (
  <View accessibilityLabel="Loading" accessibilityRole="progressbar">
    <SkeletonRow />
    <SkeletonRow />
    <SkeletonRow />
  </View>
);

/** Saved places and the caller's submissions with their review status. */
export function SavedScreen() {
  const theme = useTheme();
  const s = theme.space;
  const layout = useLayout();
  const insets = useSafeAreaInsets();
  const [tab, setTab] = useState<Tab>("saved");
  const favs = useFavoritePlaces({ enabled: tab === "saved" });
  const mine = useMyPlaces({ enabled: tab === "mine" });
  const favRows = useMemo(() => flattenPages(favs.data?.pages), [favs.data]);
  const mineRows = useMemo(() => flattenPages(mine.data?.pages), [mine.data]);
  const header = (
    <View style={{ paddingHorizontal: s[2], paddingBottom: s[3] }}>
      <SegmentedControl<Tab>
        options={[
          { value: "saved", label: "Saved" },
          { value: "mine", label: "Added by you" },
        ]}
        value={tab}
        onValueChange={setTab}
        accessibilityLabel="Your places"
      />
    </View>
  );
  const pad = { paddingHorizontal: layout.gutter - s[2], paddingTop: s[4], paddingBottom: insets.bottom + s[6], flexGrow: 1 };

  if (tab === "saved") {
    return (
      <FlatList
        data={favRows}
        keyExtractor={(p) => p.id}
        contentContainerStyle={pad}
        ListHeaderComponent={header}
        renderItem={({ item }) => <PlaceRow place={item} />}
        onEndReached={() => favs.hasNextPage && !favs.isFetchingNextPage && void favs.fetchNextPage()}
        ListFooterComponent={favs.isFetchingNextPage ? <Spinner /> : null}
        ListEmptyComponent={
          favs.isPending ? (
            loading
          ) : favs.isError ? (
            <ErrorState title="Couldn't load saved places" info={errorMessage(favs.error)} onRetry={() => void favs.refetch()} retrying={favs.isRefetching} />
          ) : (
            <EmptyState icon={Heart} title="No saved places yet" action={<Button variant="secondary" onPress={() => router.navigate("/places")}>Browse places</Button>} />
          )
        }
      />
    );
  }
  return (
    <FlatList
      data={mineRows}
      keyExtractor={(p) => p.id}
      contentContainerStyle={pad}
      ListHeaderComponent={header}
      renderItem={({ item }) => <MineRow place={item} />}
      onEndReached={() => mine.hasNextPage && !mine.isFetchingNextPage && void mine.fetchNextPage()}
      ListFooterComponent={mine.isFetchingNextPage ? <Spinner /> : null}
      ListEmptyComponent={
        mine.isPending ? (
          loading
        ) : mine.isError ? (
          <ErrorState title="Couldn't load your places" info={errorMessage(mine.error)} onRetry={() => void mine.refetch()} retrying={mine.isRefetching} />
        ) : (
          <EmptyState icon={MapPinPlus} title="You haven't added a place" action={<Button variant="secondary" leftIcon={Plus} onPress={() => router.push("/places/new")}>Add a place</Button>} />
        )
      }
    />
  );
}
