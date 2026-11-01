import { errorMessage, formatDistance, openState, useFavoriteIds, usePlacesConfig, useToggleFavorite, type PlaceHours, type PlaceStatus, type PlaceSummary } from "@scaffoldapp/shared";
import { Badge, Body, Caption, IconButton, ToggleChip, toast, useTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { router } from "expo-router";
import { Heart, MapPin } from "lucide-react-native";
import { useCallback } from "react";
import { Pressable, ScrollView, View } from "react-native";

export function useCategoryLabel(): (id: string) => string {
  const config = usePlacesConfig();
  const cats = config.data?.categories;
  return useCallback((id: string) => cats?.find((c) => c.id === id)?.label ?? id, [cats]);
}

export function OpenBadge({ hours }: { hours: PlaceHours | null }) {
  const state = openState(hours);
  if (state === "unknown") return null;
  return (
    <Badge size="sm" tone={state === "open" ? "success" : "neutral"} dot>
      {state === "open" ? "Open now" : "Closed now"}
    </Badge>
  );
}

const STATUS: Record<PlaceStatus, { label: string; tone: "warning" | "success" | "danger" }> = {
  pending: { label: "In review", tone: "warning" },
  approved: { label: "On the map", tone: "success" },
  rejected: { label: "Not approved", tone: "danger" },
};

export function PlaceStatusBadge({ status }: { status: PlaceStatus }) {
  const s = STATUS[status];
  return (
    <Badge size="sm" tone={s.tone}>
      {s.label}
    </Badge>
  );
}

export function FavoriteButton({ placeId }: { placeId: string }) {
  const theme = useTheme();
  const ids = useFavoriteIds();
  const toggle = useToggleFavorite();
  const on = ids.data?.includes(placeId) ?? false;
  return (
    <IconButton
      label={on ? "Remove from saved" : "Save place"}
      icon={<Heart size={22} color={on ? theme.colors.dangerStrong : theme.colors.text} fill={on ? theme.colors.dangerStrong : "transparent"} strokeWidth={2} />}
      accessibilityState={{ selected: on }}
      onPress={() => toggle.mutate({ id: placeId, favorite: !on }, { onError: (e) => toast.error(errorMessage(e, "Couldn't update saved places. Try again.")) })}
    />
  );
}

export function PlaceThumb({ url, size = 48 }: { url: string | null; size?: number }) {
  const theme = useTheme();
  return (
    <View style={{ width: size, height: size, borderRadius: theme.radius.md, overflow: "hidden", backgroundColor: theme.colors.sunken, alignItems: "center", justifyContent: "center" }}>
      {url ? <Image source={{ uri: url }} style={{ width: size, height: size }} contentFit="cover" accessibilityIgnoresInvertColors /> : <MapPin size={20} color={theme.colors.textMuted} />}
    </View>
  );
}

/** A place in a list: thumb · name · category, distance and address · save. */
export function PlaceRow({ place, selected, onPress }: { place: PlaceSummary; selected?: boolean; onPress?: () => void }) {
  const theme = useTheme();
  const s = theme.space;
  const label = useCategoryLabel();
  const meta = [label(place.category), formatDistance(place.distance_m), place.address].filter(Boolean).join(" · ");
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: s[3], paddingVertical: s[2], paddingHorizontal: s[2], borderRadius: theme.radius.md, backgroundColor: selected ? theme.colors.primarySoft : "transparent" }}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={place.name}
        onPress={onPress ?? (() => router.push({ pathname: "/places/[id]", params: { id: place.id } }))}
        style={{ flex: 1, flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.base }}
      >
        <PlaceThumb url={place.photo_url} />
        <View style={{ flex: 1, gap: s[0.5] }}>
          <Body weight={600} numberOfLines={1}>
            {place.name}
          </Body>
          {meta ? (
            <Caption tone="muted" numberOfLines={1}>
              {meta}
            </Caption>
          ) : null}
        </View>
      </Pressable>
      <FavoriteButton placeId={place.id} />
    </View>
  );
}

export function CategoryChips({ value, onChange }: { value: readonly string[]; onChange: (next: string[]) => void }) {
  const theme = useTheme();
  const config = usePlacesConfig();
  const cats = config.data?.categories ?? [];
  if (cats.length === 0) return null;
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: theme.space[2] }} accessibilityLabel="Categories">
      {cats.map((c) => {
        const on = value.includes(c.id);
        return (
          <ToggleChip key={c.id} size="sm" pressed={on} onPressedChange={(p) => onChange(p ? [...value, c.id] : value.filter((x) => x !== c.id))}>
            {c.label}
          </ToggleChip>
        );
      })}
    </ScrollView>
  );
}
