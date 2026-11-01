import { withAlpha } from "@scaffoldapp/tokens";
import { Image } from "expo-image";
import { LinearGradient } from "expo-linear-gradient";
import { ImageOff } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { Pressable, View, type StyleProp, type ViewStyle } from "react-native";
import Animated from "react-native-reanimated";
import { haptic } from "../lib/haptics";
import { enterUp } from "../lib/motion";
import { usePressScale } from "../lib/press";
import { useTheme } from "../theme/ThemeProvider";
import { PhotoLightbox, type GalleryPhoto } from "./PhotoLightbox";
import { Skeleton } from "./Skeleton";
import { Text } from "./Text";

export interface PhotoGridProps {
  photos: GalleryPhoto[];
  /** @default 3 */
  columns?: 2 | 3 | 4;
  /** Built-in lightbox on press (else handle `onOpen`). @default true */
  lightbox?: boolean;
  onOpen?: (index: number) => void;
  loading?: boolean;
  /** Rendered when there are no photos (callers own the empty state). */
  empty?: ReactNode;
  style?: StyleProp<ViewStyle>;
}

function Tile({ photo, size, index, onPress }: { photo: GalleryPhoto; size: number; index: number; onPress: () => void }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const press = usePressScale(0.96);
  const [broken, setBroken] = useState(false);
  return (
    <Animated.View entering={enterUp(theme, index)}>
      <Pressable
        onPress={() => {
          haptic.light();
          onPress();
        }}
        onPressIn={press.onPressIn}
        onPressOut={press.onPressOut}
        accessibilityRole="imagebutton"
        accessibilityLabel={photo.caption || `Photo ${index + 1}`}
      >
        <Animated.View style={[{ width: size, height: size, borderRadius: theme.radius.lg, overflow: "hidden", backgroundColor: c.sunken }, press.style]}>
          {broken ? (
            <View style={{ flex: 1, alignItems: "center", justifyContent: "center" }}>
              <ImageOff size={24} color={c.textMuted} strokeWidth={2} />
            </View>
          ) : (
            <Image source={{ uri: photo.url }} style={{ width: "100%", height: "100%" }} contentFit="cover" transition={theme.motion.duration.base} onError={() => setBroken(true)} />
          )}
          {photo.caption ? (
            <LinearGradient colors={["transparent", withAlpha(c.ink[950], 0.85)]} style={{ position: "absolute", left: 0, right: 0, bottom: 0, paddingTop: s[6], paddingHorizontal: s[2], paddingBottom: s[2] }}>
              <Text role="caption" weight={600} numberOfLines={2} style={{ color: c.ink[50] }}>
                {photo.caption}
              </Text>
            </LinearGradient>
          ) : null}
        </Animated.View>
      </Pressable>
    </Animated.View>
  );
}

/** Square photo tiles with caption scrims; pressing one opens the lightbox. */
export function PhotoGrid({ photos, columns = 3, lightbox = true, onOpen, loading, empty, style }: PhotoGridProps) {
  const theme = useTheme();
  const gap = theme.space[2];
  const [w, setW] = useState(0);
  const [open, setOpen] = useState<number | null>(null);
  const size = w > 0 ? Math.floor((w - gap * (columns - 1)) / columns) : 0;
  if (!loading && photos.length === 0) return <>{empty ?? null}</>;
  return (
    <View style={style} onLayout={(e) => setW(e.nativeEvent.layout.width)} accessibilityLabel="Photos">
      {size > 0 ? (
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap }}>
          {loading
            ? Array.from({ length: columns }, (_, k) => <Skeleton key={k} width={size} height={size} style={{ borderRadius: theme.radius.lg }} />)
            : photos.map((p, k) => (
                <Tile
                  key={p.id}
                  photo={p}
                  index={k}
                  size={size}
                  onPress={() => {
                    onOpen?.(k);
                    if (lightbox) setOpen(k);
                  }}
                />
              ))}
        </View>
      ) : null}
      {lightbox ? <PhotoLightbox photos={photos} open={open !== null} initialIndex={open ?? 0} onClose={() => setOpen(null)} /> : null}
    </View>
  );
}
