import { withAlpha } from "@scaffoldapp/tokens";
import { Image } from "expo-image";
import * as ImagePicker from "expo-image-picker";
import { Camera, ImageOff, ImagePlus, Images, RotateCw, X } from "lucide-react-native";
import { useCallback, useEffect, useRef, useState } from "react";
import { Alert, Linking, Pressable, View, type StyleProp, type ViewStyle } from "react-native";
import Animated, { ZoomOut } from "react-native-reanimated";
import Svg, { Circle } from "react-native-svg";
import { haptic } from "../lib/haptics";
import { layoutSpring, popIn } from "../lib/motion";
import { usePressScale } from "../lib/press";
import { useTheme } from "../theme/ThemeProvider";
import { Input } from "./Input";
import { Text } from "./Text";

/** A picked photo (accepted by `@scaffoldapp/shared` `useUpload` as `file`). */
export interface PickedPhoto {
  uri: string;
  width?: number;
  height?: number;
  mimeType?: string | null;
  name?: string | null;
  size?: number | null;
}

export interface PhotoUploadResult {
  key: string;
  url?: string | null;
}

export interface UploadedPhoto {
  id: string;
  key: string;
  url: string | null;
  /** Local file of the picked photo (instant previews). */
  localUri?: string;
  caption?: string;
}

export interface PhotoUploaderProps {
  /**
   * Upload one photo: `useUpload().mutateAsync({ file, purpose, signal })`. Report
   * `onProgress(0–1)` if you can (an estimate shows otherwise); honour `signal` so removing
   * a tile cancels its upload.
   */
  upload: (file: PickedPhoto, ctx: { signal: AbortSignal; onProgress: (fraction: number) => void }) => Promise<PhotoUploadResult>;
  defaultValue?: UploadedPhoto[];
  /** Every uploaded photo, in order, whenever the set or a caption changes. */
  onChange?: (photos: UploadedPhoto[]) => void;
  onUploadingChange?: (uploading: boolean) => void;
  /** @default 6 */
  max?: number;
  /** @default ["camera", "library"] */
  sources?: ("camera" | "library")[];
  /** JPEG quality (< 1 also converts HEIC). @default 0.7 */
  quality?: number;
  captions?: boolean;
  /** @default 200 */
  captionMaxLength?: number;
  /** @default 96 */
  tileSize?: number;
  disabled?: boolean;
  style?: StyleProp<ViewStyle>;
}

interface Item {
  id: string;
  file: PickedPhoto | null;
  uri: string | null;
  status: "uploading" | "done" | "error";
  progress: number;
  reported: boolean;
  key: string | null;
  url: string | null;
  caption: string;
  broken: boolean;
  controller: AbortController | null;
}

let seq = 0;

function settingsNudge() {
  Alert.alert("Camera access is off", "Turn it on in Settings to take photos.", [
    { text: "Not now", style: "cancel" },
    { text: "Open Settings", onPress: () => void Linking.openSettings() },
  ]);
}

/**
 * Pick photos: the system photo picker (no library permission) or the camera (asked at
 * the moment of use, with a Settings nudge when denied). Returns [] when canceled.
 */
export async function pickPhotos(source: "camera" | "library", limit = 1, quality = 0.7): Promise<PickedPhoto[]> {
  try {
    if (source === "camera") {
      const perm = await ImagePicker.requestCameraPermissionsAsync();
      if (!perm.granted) {
        if (!perm.canAskAgain) settingsNudge();
        return [];
      }
    }
    const options: ImagePicker.ImagePickerOptions = {
      mediaTypes: ["images"],
      quality,
      exif: false,
      ...(source === "library" && limit > 1 ? { allowsMultipleSelection: true, selectionLimit: limit } : {}),
    };
    const res = source === "camera" ? await ImagePicker.launchCameraAsync(options) : await ImagePicker.launchImageLibraryAsync(options);
    if (res.canceled) return [];
    return res.assets.slice(0, limit).map((a) => ({ uri: a.uri, width: a.width, height: a.height, mimeType: a.mimeType ?? "image/jpeg", name: a.fileName ?? null, size: a.fileSize ?? null }));
  } catch {
    return [];
  }
}

function Ring({ value, color, size = 40 }: { value: number; color: string; size?: number }) {
  const r = (size - 6) / 2;
  const circ = 2 * Math.PI * r;
  return (
    <Svg width={size} height={size} style={{ transform: [{ rotate: "-90deg" }] }}>
      <Circle cx={size / 2} cy={size / 2} r={r} stroke={color} strokeOpacity={0.28} strokeWidth={4} fill="none" />
      <Circle cx={size / 2} cy={size / 2} r={r} stroke={color} strokeWidth={4} fill="none" strokeLinecap="round" strokeDasharray={`${circ}`} strokeDashoffset={circ * (1 - Math.max(0.04, Math.min(1, value)))} />
    </Svg>
  );
}

function ActionTile({ icon: Icon, label, onPress, primary, size, disabled }: { icon: typeof Camera; label: string; onPress: () => void; primary?: boolean; size: number; disabled?: boolean }) {
  const theme = useTheme();
  const c = theme.colors;
  const press = usePressScale(0.95);
  const fg = primary ? c.primaryStrong : c.textMuted;
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={label} disabled={disabled} onPressIn={press.onPressIn} onPressOut={press.onPressOut} onPress={onPress}>
      <Animated.View
        style={[
          {
            width: size,
            minHeight: size,
            padding: theme.space[2],
            borderRadius: theme.radius.lg,
            borderWidth: 1,
            borderStyle: "dashed",
            borderColor: primary ? c.primaryStrong : c.borderStrong,
            backgroundColor: primary ? c.primarySoft : "transparent",
            alignItems: "center",
            justifyContent: "center",
            gap: theme.space[1],
            opacity: disabled ? 0.5 : 1,
          },
          press.style,
        ]}
      >
        <Icon size={24} color={fg} strokeWidth={2} />
        <Text role="label" weight={600} align="center" style={{ color: fg }}>
          {label}
        </Text>
      </Animated.View>
    </Pressable>
  );
}

/**
 * Photo picker + uploader: camera and library tiles, instant previews, per-photo progress,
 * retry, remove (cancels the upload), a max count and optional captions.
 *
 * ```tsx
 * const upload = useUpload();
 * <PhotoUploader upload={(file, { signal }) => upload.mutateAsync({ file, purpose: "post_media", signal }).then((r) => ({ key: r.key, url: r.public_url }))} onChange={setPhotos} />
 * ```
 */
export function PhotoUploader({
  upload,
  defaultValue,
  onChange,
  onUploadingChange,
  max = 6,
  sources = ["camera", "library"],
  quality = 0.7,
  captions = false,
  captionMaxLength = 200,
  tileSize = 96,
  disabled = false,
  style,
}: PhotoUploaderProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [items, setItems] = useState<Item[]>(() =>
    (defaultValue ?? []).map((p) => ({ id: p.id, file: null, uri: p.localUri ?? p.url, status: "done", progress: 1, reported: true, key: p.key, url: p.url, caption: p.caption ?? "", broken: false, controller: null })),
  );
  const uploadRef = useRef(upload);
  uploadRef.current = upload;
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const onUploadingRef = useRef(onUploadingChange);
  onUploadingRef.current = onUploadingChange;
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const patch = useCallback((id: string, p: Partial<Item>) => {
    if (mounted.current) setItems((prev) => prev.map((it) => (it.id === id ? { ...it, ...p } : it)));
  }, []);

  const doneSig = items
    .filter((i) => i.status === "done")
    .map((i) => `${i.id}:${i.key}:${i.caption}`)
    .join("|");
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    onChangeRef.current?.(
      items.flatMap((i) => (i.status === "done" && i.key ? [{ id: i.id, key: i.key, url: i.url, ...(i.file ? { localUri: i.file.uri } : {}), ...(i.caption ? { caption: i.caption } : {}) }] : [])),
    );
    // Fires when the uploaded set or a caption changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [doneSig]);
  const uploading = items.some((i) => i.status === "uploading");
  useEffect(() => {
    onUploadingRef.current?.(uploading);
  }, [uploading]);

  const estimating = items.some((i) => i.status === "uploading" && !i.reported);
  useEffect(() => {
    if (!estimating) return undefined;
    const id = setInterval(() => {
      setItems((prev) => prev.map((i) => (i.status === "uploading" && !i.reported ? { ...i, progress: i.progress + (0.9 - i.progress) * 0.12 } : i)));
    }, 300);
    return () => clearInterval(id);
  }, [estimating]);

  const start = useCallback(
    (item: Item) => {
      if (!item.file) return;
      const controller = new AbortController();
      patch(item.id, { status: "uploading", progress: 0.03, reported: false, controller });
      uploadRef
        .current(item.file, { signal: controller.signal, onProgress: (f) => patch(item.id, { progress: Math.max(0.03, Math.min(1, f)), reported: true }) })
        .then((res) => {
          if (controller.signal.aborted) return;
          haptic.success();
          patch(item.id, { status: "done", progress: 1, key: res.key, url: res.url ?? null, controller: null });
        })
        .catch(() => {
          if (controller.signal.aborted) return;
          haptic.error();
          patch(item.id, { status: "error", controller: null });
        });
    },
    [patch],
  );

  const room = Math.max(0, max - items.length);
  const add = async (source: "camera" | "library") => {
    const picked = await pickPhotos(source, room, quality);
    if (!picked.length || !mounted.current) return;
    haptic.light();
    const fresh = picked.slice(0, room).map<Item>((file) => ({ id: `ph${++seq}`, file, uri: file.uri, status: "uploading", progress: 0.03, reported: false, key: null, url: null, caption: "", broken: false, controller: null }));
    setItems((prev) => [...prev, ...fresh].slice(0, max));
    fresh.forEach(start);
  };

  const remove = (id: string) => {
    haptic.light();
    setItems((prev) => {
      prev.find((i) => i.id === id)?.controller?.abort();
      return prev.filter((i) => i.id !== id);
    });
  };

  const layout = layoutSpring(theme);
  const removeSize = s[8];
  const scrim = withAlpha(c.ink[950], 0.6);

  return (
    <Animated.View layout={layout} style={[{ flexDirection: "row", flexWrap: "wrap", gap: s[3] }, style]} accessibilityLabel="Photos">
      {items.map((it) => (
        <Animated.View key={it.id} entering={popIn(theme)} exiting={ZoomOut.duration(theme.motion.duration.fast)} layout={layout} style={{ width: tileSize, gap: s[2] }}>
          <View style={{ width: tileSize, height: tileSize, borderRadius: theme.radius.lg, overflow: "hidden", backgroundColor: c.sunken }}>
            {it.uri && !it.broken ? (
              <Image source={{ uri: it.uri }} style={{ width: "100%", height: "100%" }} contentFit="cover" transition={theme.motion.duration.base} onError={() => patch(it.id, { broken: true })} accessibilityLabel={it.caption || "Photo"} />
            ) : (
              <View style={{ flex: 1, alignItems: "center", justifyContent: "center" }}>
                <ImageOff size={24} color={c.textMuted} strokeWidth={2} />
              </View>
            )}
            {it.status === "uploading" ? (
              <View
                style={{ position: "absolute", inset: 0, alignItems: "center", justifyContent: "center", backgroundColor: scrim }}
                accessibilityRole="progressbar"
                accessibilityLabel="Uploading"
                accessibilityValue={{ min: 0, max: 100, now: Math.round(it.progress * 100) }}
              >
                <Ring value={it.progress} color={c.ink[50]} />
              </View>
            ) : it.status === "error" ? (
              <Pressable
                onPress={() => start(it)}
                accessibilityRole="button"
                accessibilityLabel="Retry upload"
                style={{ position: "absolute", inset: 0, alignItems: "center", justifyContent: "center", gap: s[1], backgroundColor: c.danger }}
              >
                <RotateCw size={22} color={c.dangerFg} strokeWidth={2.5} />
                <Text role="label" weight={600} style={{ color: c.dangerFg }}>
                  Retry
                </Text>
              </Pressable>
            ) : null}
            <Pressable
              accessibilityRole="button"
              accessibilityLabel="Remove photo"
              hitSlop={(theme.tap.base - removeSize) / 2}
              disabled={disabled}
              onPress={() => remove(it.id)}
              style={{ position: "absolute", top: s[1], right: s[1], width: removeSize, height: removeSize, borderRadius: removeSize / 2, backgroundColor: scrim, alignItems: "center", justifyContent: "center" }}
            >
              <X size={16} color={c.ink[50]} strokeWidth={2.75} />
            </Pressable>
          </View>
          {captions && it.status === "done" ? (
            <Input value={it.caption} maxLength={captionMaxLength} placeholder="Caption" accessibilityLabel="Photo caption" onChangeText={(caption) => patch(it.id, { caption })} />
          ) : null}
        </Animated.View>
      ))}
      {room > 0 ? (
        <Animated.View layout={layout} style={{ flexDirection: "row", gap: s[3] }}>
          {sources.includes("camera") ? <ActionTile icon={Camera} label="Camera" primary onPress={() => void add("camera")} size={tileSize} disabled={disabled} /> : null}
          {sources.includes("library") ? (
            <ActionTile icon={sources.includes("camera") ? Images : ImagePlus} label={sources.includes("camera") ? "Library" : "Add photos"} onPress={() => void add("library")} size={tileSize} disabled={disabled} />
          ) : null}
        </Animated.View>
      ) : null}
    </Animated.View>
  );
}
