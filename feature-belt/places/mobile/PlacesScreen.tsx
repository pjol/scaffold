import { errorMessage, usePlace, usePlaceClusters, usePlaceSearch, usePlacesConfig, useDebouncedValue, flattenPages, type LatLng, type PlaceSummary } from "@scaffoldapp/shared";
import {
  Body,
  Button,
  Caption,
  Card,
  EmptyState,
  ErrorState,
  IconButton,
  Input,
  SegmentedControl,
  Skeleton,
  SkeletonRow,
  Spinner,
  Title,
  useLayout,
  useTheme,
} from "@scaffoldapp/ui-native";
import { router, useScrollToTop } from "expo-router";
import { Bookmark, LocateFixed, MapPin, Plus, Search, SearchX, X } from "lucide-react-native";
import { useMemo, useRef, useState } from "react";
import { FlatList, View, useWindowDimensions } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { deviceLocation } from "./location";
import { CategoryChips, FavoriteButton, OpenBadge, PlaceRow, PlaceThumb, useCategoryLabel } from "./parts";
import { PlacesMap, type NativePin, type NativeViewport, type PlacesMapHandle } from "./PlacesMap";

type Mode = "map" | "list";

/** Approximate visible box before the map reports its first camera move. */
function initialViewport(center: LatLng, zoom: number, width: number, height: number): NativeViewport {
  const lngSpan = (360 / 2 ** zoom) * (width / 256);
  const latSpan = lngSpan * (height / width) * Math.cos((center.lat * Math.PI) / 180);
  return { center, zoom, bbox: [center.lng - lngSpan / 2, Math.max(-90, center.lat - latSpan / 2), center.lng + lngSpan / 2, Math.min(90, center.lat + latSpan / 2)] };
}

function Preview({ id, onClose }: { id: string; onClose: () => void }) {
  const theme = useTheme();
  const s = theme.space;
  const place = usePlace(id);
  const label = useCategoryLabel();
  return (
    <Card padding="compact" variant="raised" style={{ position: "absolute", left: s[4], right: s[4], bottom: s[4] }}>
      <View style={{ gap: s[3] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[3] }}>
        {place.data ? (
          <>
            <PlaceThumb url={place.data.photos[0]?.url ?? null} size={56} />
            <View style={{ flex: 1, gap: s[1] }}>
              <Body weight={600} numberOfLines={1}>
                {place.data.name}
              </Body>
              <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: s[2] }}>
                <Caption tone="muted">{label(place.data.category)}</Caption>
                <OpenBadge hours={place.data.hours} />
              </View>
            </View>
            <FavoriteButton placeId={id} />
          </>
        ) : place.isError ? (
          <Body tone="muted" style={{ flex: 1 }}>
            {errorMessage(place.error, "Couldn't load this place.")}
          </Body>
        ) : (
          <View style={{ flex: 1, flexDirection: "row", gap: s[3], alignItems: "center" }}>
            <Skeleton width={56} height={56} />
            <Skeleton shape="text" width="60%" height={16} />
          </View>
        )}
        <IconButton label="Close" icon={X} onPress={onClose} />
      </View>
      {place.data ? (
        <Button variant="secondary" size="md" onPress={() => router.push({ pathname: "/places/[id]", params: { id } })}>
          Details
        </Button>
      ) : null}
      </View>
    </Card>
  );
}

/** Places tab: map (Apple Maps / Google Maps) or list of the same area, search and categories. */
export function PlacesScreen() {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const layout = useLayout();
  const insets = useSafeAreaInsets();
  const { width, height } = useWindowDimensions();
  const config = usePlacesConfig();
  const [mode, setMode] = useState<Mode>("map");
  const [text, setText] = useState("");
  const q = useDebouncedValue(text.trim(), 300);
  const [categories, setCategories] = useState<string[]>([]);
  const [viewport, setViewport] = useState<NativeViewport | null>(null);
  const [near, setNear] = useState<LatLng | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [epoch, setEpoch] = useState(0);
  const [locating, setLocating] = useState(false);
  const map = useRef<PlacesMapHandle>(null);
  const listRef = useRef<FlatList<PlaceSummary>>(null);
  useScrollToTop(listRef);

  const view = viewport ?? (config.data ? initialViewport(config.data.default_center, config.data.default_zoom, width, height * 0.6) : null);
  const clusters = usePlaceClusters(view ? { bbox: view.bbox, zoom: view.zoom, category: categories, q } : null);
  const list = usePlaceSearch({ bbox: view?.bbox, category: categories, q, near, limit: 30 }, { enabled: !!view && mode === "list" });
  const rows = useMemo(() => flattenPages(list.data?.pages), [list.data]);

  const pins = useMemo<NativePin[]>(() => {
    const d = clusters.data;
    if (!d) return [];
    return [...d.clusters.map((x) => ({ id: `c:${x.id}`, lat: x.lat, lng: x.lng, count: x.count, title: `${x.count} places` })), ...d.places.map((p) => ({ id: p.id, lat: p.lat, lng: p.lng, title: p.name }))];
  }, [clusters.data]);

  const onPin = (id: string) => {
    if (id.startsWith("c:")) {
      const cl = clusters.data?.clusters.find((x) => `c:${x.id}` === id);
      if (cl) map.current?.moveTo({ lat: cl.lat, lng: cl.lng }, Math.min(20, (view?.zoom ?? 12) + 2));
      return;
    }
    setSelected(id);
  };

  const locate = async () => {
    setLocating(true);
    const p = await deviceLocation();
    setLocating(false);
    if (!p) return;
    setNear(p);
    map.current?.moveTo(p, Math.max(view?.zoom ?? 14, 14));
  };

  if (config.isPending) {
    return (
      <View style={{ flex: 1, paddingTop: insets.top + s[4], paddingHorizontal: layout.gutter, gap: s[4] }}>
        <Skeleton shape="text" width="40%" height={theme.type.title.lineHeight} />
        <Skeleton height={theme.tap.base} />
        <Skeleton height={height * 0.5} />
      </View>
    );
  }
  if (config.isError || !config.data) {
    return (
      <View style={{ flex: 1, paddingTop: insets.top + s[4], paddingHorizontal: layout.gutter, justifyContent: "center" }}>
        <ErrorState title="Couldn't load places" info={errorMessage(config.error)} onRetry={() => void config.refetch()} retrying={config.isRefetching} />
      </View>
    );
  }
  const filtered = !!q || categories.length > 0;

  return (
    <View style={{ flex: 1, backgroundColor: c.canvas }}>
      <View style={{ paddingTop: insets.top + s[4], paddingHorizontal: layout.gutter, paddingBottom: s[3], gap: s[3] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Title style={{ flex: 1 }}>Places</Title>
          <IconButton label="Saved places" icon={Bookmark} onPress={() => router.push("/places/saved")} />
          <IconButton label="Add a place" icon={Plus} variant="primary" onPress={() => router.push("/places/new")} />
        </View>
        <Input value={text} onChangeText={setText} placeholder="Name or address" leadingIcon={Search} returnKeyType="search" accessibilityLabel="Search places" clearButtonMode="while-editing" />
        <CategoryChips value={categories} onChange={setCategories} />
        <SegmentedControl<Mode>
          options={[
            { value: "map", label: "Map" },
            { value: "list", label: "List" },
          ]}
          value={mode}
          onValueChange={setMode}
          accessibilityLabel="View"
        />
      </View>

      {mode === "map" ? (
        <View style={{ flex: 1 }}>
          <PlacesMap ref={map} pins={pins} initial={{ center: config.data.default_center, zoom: config.data.default_zoom }} onViewport={setViewport} onPinPress={onPin} showsUser={!!near} epoch={epoch} />
          <View style={{ position: "absolute", top: s[3], right: s[3], gap: s[2] }}>
            <IconButton label="Show my location" icon={locating ? <Spinner size={18} /> : LocateFixed} variant="secondary" onPress={() => void locate()} />
          </View>
          {clusters.data?.truncated ? (
            <View style={{ position: "absolute", top: s[3], left: s[3], right: s[16] }}>
              <Card padding="compact" variant="raised">
                <Caption>Zoom in to see every place.</Caption>
              </Card>
            </View>
          ) : null}
          {selected ? (
            <Preview
              id={selected}
              onClose={() => {
                setSelected(null);
                setEpoch((e) => e + 1);
              }}
            />
          ) : null}
        </View>
      ) : (
        <FlatList
          ref={listRef}
          data={rows}
          keyExtractor={(p) => p.id}
          contentContainerStyle={{ paddingHorizontal: layout.gutter - s[2], paddingBottom: s[6], gap: s[1], flexGrow: 1 }}
          renderItem={({ item }) => <PlaceRow place={item} />}
          onEndReached={() => list.hasNextPage && !list.isFetchingNextPage && void list.fetchNextPage()}
          onEndReachedThreshold={0.5}
          ListHeaderComponent={
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], paddingHorizontal: s[2], paddingBottom: s[2] }}>
              <Caption tone="muted" style={{ flex: 1 }}>
                {near ? "Nearest first" : "In the map area"}
              </Caption>
              {near ? (
                <Button variant="ghost" size="md" onPress={() => setNear(null)}>
                  Newest first
                </Button>
              ) : (
                <Button variant="ghost" size="md" leftIcon={LocateFixed} loading={locating} onPress={() => void locate()}>
                  Near me
                </Button>
              )}
            </View>
          }
          ListEmptyComponent={
            list.isPending ? (
              <View accessibilityLabel="Loading" accessibilityRole="progressbar">
                <SkeletonRow />
                <SkeletonRow />
                <SkeletonRow />
              </View>
            ) : list.isError ? (
              <ErrorState title="Couldn't load places" info={errorMessage(list.error)} onRetry={() => void list.refetch()} retrying={list.isRefetching} />
            ) : (
              <EmptyState
                icon={filtered ? SearchX : MapPin}
                title={filtered ? "No matching places here" : "No places in this area"}
                action={
                  filtered ? (
                    <Button
                      variant="secondary"
                      onPress={() => {
                        setText("");
                        setCategories([]);
                      }}
                    >
                      Clear filters
                    </Button>
                  ) : (
                    <Button variant="secondary" leftIcon={Plus} onPress={() => router.push("/places/new")}>
                      Add a place
                    </Button>
                  )
                }
              />
            )
          }
          ListFooterComponent={list.isFetchingNextPage ? <Spinner /> : null}
        />
      )}
    </View>
  );
}
