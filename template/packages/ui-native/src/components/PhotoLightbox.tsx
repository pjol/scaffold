import { withAlpha } from "@scaffoldapp/tokens";
import { Image } from "expo-image";
import { ImageOff, X } from "lucide-react-native";
import { useCallback, useEffect, useState } from "react";
import { FlatList, Modal, Pressable, StatusBar, View, useWindowDimensions, type NativeScrollEvent, type NativeSyntheticEvent } from "react-native";
import { Gesture, GestureDetector, GestureHandlerRootView } from "react-native-gesture-handler";
import Animated, { useAnimatedStyle, useSharedValue, withSpring } from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { scheduleOnRN } from "react-native-worklets";
import { haptic } from "../lib/haptics";
import { fadeIn, fadeOut } from "../lib/motion";
import { useTheme } from "../theme/ThemeProvider";
import { Text } from "./Text";

/** A photo to show: only `id` and `url` are required. */
export interface GalleryPhoto {
  id: string;
  url: string;
  caption?: string | null;
  created_at?: string | null;
}

export interface PhotoLightboxProps {
  photos: GalleryPhoto[];
  open: boolean;
  onClose: () => void;
  /** @default 0 */
  initialIndex?: number;
  onIndexChange?: (index: number) => void;
}

const MAX_ZOOM = 4;

function formatPhotoTime(iso: string | null | undefined): string | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return null;
  return d.toLocaleString([], { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

function ZoomablePhoto({ photo, width, height, onZoomChange }: { photo: GalleryPhoto; width: number; height: number; onZoomChange: (zoomed: boolean) => void }) {
  const theme = useTheme();
  const spring = theme.motion.spring.snappy;
  const [broken, setBroken] = useState(false);
  const [zoomed, setZoomed] = useState(false);
  const scale = useSharedValue(1);
  const saved = useSharedValue(1);
  const tx = useSharedValue(0);
  const ty = useSharedValue(0);
  const sx = useSharedValue(0);
  const sy = useSharedValue(0);
  const report = useCallback(
    (z: boolean) => {
      setZoomed(z);
      onZoomChange(z);
    },
    [onZoomChange],
  );

  const pinch = Gesture.Pinch()
    .onUpdate((e) => {
      scale.value = Math.min(MAX_ZOOM, Math.max(1, saved.value * e.scale));
    })
    .onEnd(() => {
      if (scale.value < 1.05) {
        scale.value = withSpring(1, spring);
        tx.value = withSpring(0, spring);
        ty.value = withSpring(0, spring);
        saved.value = 1;
        sx.value = 0;
        sy.value = 0;
        scheduleOnRN(report, false);
      } else {
        saved.value = scale.value;
        scheduleOnRN(report, true);
      }
    });

  const pan = Gesture.Pan()
    .enabled(zoomed)
    .onUpdate((e) => {
      const mx = (width * (scale.value - 1)) / 2;
      const my = (height * (scale.value - 1)) / 2;
      tx.value = Math.min(mx, Math.max(-mx, sx.value + e.translationX));
      ty.value = Math.min(my, Math.max(-my, sy.value + e.translationY));
    })
    .onEnd(() => {
      sx.value = tx.value;
      sy.value = ty.value;
    });

  const doubleTap = Gesture.Tap()
    .numberOfTaps(2)
    .onEnd(() => {
      const next = saved.value > 1 ? 1 : 2.5;
      scale.value = withSpring(next, spring);
      saved.value = next;
      if (next === 1) {
        tx.value = withSpring(0, spring);
        ty.value = withSpring(0, spring);
        sx.value = 0;
        sy.value = 0;
      }
      scheduleOnRN(report, next > 1);
    });

  const style = useAnimatedStyle(() => ({ transform: [{ translateX: tx.value }, { translateY: ty.value }, { scale: scale.value }] }));

  return (
    <GestureDetector gesture={Gesture.Simultaneous(pinch, pan, doubleTap)}>
      <View style={{ width, height, alignItems: "center", justifyContent: "center" }}>
        {broken ? (
          <View style={{ alignItems: "center", gap: theme.space[2] }}>
            <ImageOff size={40} color={theme.colors.ink[300]} strokeWidth={1.75} />
            <Text weight={500} style={{ color: theme.colors.ink[300] }}>
              Photo unavailable
            </Text>
          </View>
        ) : (
          <Animated.View style={[{ width, height }, style]}>
            <Image source={{ uri: photo.url }} style={{ width: "100%", height: "100%" }} contentFit="contain" transition={theme.motion.duration.base} onError={() => setBroken(true)} accessibilityLabel={photo.caption || "Photo"} />
          </Animated.View>
        )}
      </View>
    </GestureDetector>
  );
}

/** Full-screen viewer on a dark stage: swipe between photos, pinch or double-tap to zoom, drag to pan. */
export function PhotoLightbox({ photos, open, onClose, initialIndex = 0, onIndexChange }: PhotoLightboxProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const insets = useSafeAreaInsets();
  const { width, height } = useWindowDimensions();
  const [index, setIndex] = useState(initialIndex);
  const [zoomed, setZoomed] = useState(false);

  useEffect(() => {
    if (open) {
      setIndex(initialIndex);
      setZoomed(false);
    }
  }, [open, initialIndex]);

  const photo = photos[Math.min(index, photos.length - 1)];
  const time = formatPhotoTime(photo?.created_at);
  const stageH = Math.max(200, height - insets.top - insets.bottom - s[32]);

  const onScrollEnd = (e: NativeSyntheticEvent<NativeScrollEvent>) => {
    const next = Math.round(e.nativeEvent.contentOffset.x / width);
    if (next !== index) {
      haptic.selection();
      setIndex(next);
      onIndexChange?.(next);
    }
  };

  if (!open || !photo) return null;
  const chrome = withAlpha(c.ink[50], 0.14);

  return (
    <Modal visible transparent animationType="none" statusBarTranslucent navigationBarTranslucent onRequestClose={onClose}>
      <StatusBar barStyle="light-content" />
      <GestureHandlerRootView style={{ flex: 1 }}>
        <Animated.View entering={fadeIn(theme)} exiting={fadeOut(theme)} style={{ flex: 1, backgroundColor: withAlpha(c.ink[950], 0.97) }} accessibilityViewIsModal>
          <View style={{ paddingTop: insets.top + s[2], paddingHorizontal: s[4], flexDirection: "row", alignItems: "center", gap: s[2] }}>
            <View style={{ borderRadius: theme.radius.pill, paddingHorizontal: s[3], paddingVertical: s[1], backgroundColor: chrome }}>
              <Text role="label" weight={600} tabular style={{ color: c.ink[50] }} accessibilityLiveRegion="polite">
                {`${index + 1} / ${photos.length}`}
              </Text>
            </View>
            <Pressable
              onPress={onClose}
              accessibilityRole="button"
              accessibilityLabel="Close"
              style={{ marginLeft: "auto", width: theme.tap.base, height: theme.tap.base, borderRadius: theme.tap.base / 2, alignItems: "center", justifyContent: "center", backgroundColor: chrome }}
            >
              <X size={24} color={c.ink[50]} strokeWidth={2.5} />
            </Pressable>
          </View>
          <FlatList
            data={photos}
            keyExtractor={(p) => p.id}
            horizontal
            pagingEnabled
            scrollEnabled={!zoomed}
            showsHorizontalScrollIndicator={false}
            initialScrollIndex={Math.min(initialIndex, photos.length - 1)}
            getItemLayout={(_, i) => ({ length: width, offset: width * i, index: i })}
            onMomentumScrollEnd={onScrollEnd}
            style={{ flexGrow: 0, marginTop: s[3] }}
            renderItem={({ item }) => <ZoomablePhoto photo={item} width={width} height={stageH} onZoomChange={setZoomed} />}
          />
          <View style={{ flex: 1, justifyContent: "flex-end", paddingHorizontal: s[4], paddingBottom: insets.bottom + s[4] }}>
            <Animated.View key={photo.id} entering={fadeIn(theme)} style={{ flexDirection: "row", alignItems: "flex-end", gap: s[3] }}>
              {photo.caption ? (
                <Text weight={500} style={{ flex: 1, color: c.ink[50] }}>
                  {photo.caption}
                </Text>
              ) : (
                <View style={{ flex: 1 }} />
              )}
              {time ? (
                <Text role="label" tabular style={{ color: c.ink[300] }}>
                  {time}
                </Text>
              ) : null}
            </Animated.View>
          </View>
        </Animated.View>
      </GestureHandlerRootView>
    </Modal>
  );
}
