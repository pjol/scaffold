import { Image } from "expo-image";
import { useState } from "react";
import { View, type StyleProp, type ViewStyle } from "react-native";
import { useTheme } from "../theme/ThemeProvider";
import { Text } from "./Text";

const SIZES = { xs: 24, sm: 32, md: 40, lg: 48, xl: 64, "2xl": 96 } as const;
export type AvatarSize = keyof typeof SIZES;

export interface AvatarProps {
  src?: string | null | undefined;
  /** Used for initials and the accessible name. */
  name: string;
  size?: AvatarSize;
  /** Surface-coloured ring (for stacks). */
  ring?: boolean;
  style?: StyleProp<ViewStyle>;
}

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  return ((parts[0]?.[0] ?? "") + (parts.length > 1 ? (parts[parts.length - 1]?.[0] ?? "") : "")).toUpperCase() || "?";
}

/** Photo with an initials fallback tinted from the intent soft colours (stable per name). */
export function Avatar({ src, name, size = "md", ring, style }: AvatarProps) {
  const theme = useTheme();
  const c = theme.colors;
  const [failed, setFailed] = useState<string | null>(null);
  const d = SIZES[size];
  const tints = [
    [c.primarySoft, c.primaryStrong],
    [c.accentSoft, c.accentStrong],
    [c.infoSoft, c.infoStrong],
    [c.successSoft, c.successStrong],
    [c.warningSoft, c.warningStrong],
  ] as const;
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) | 0;
  const [bg, fg] = tints[Math.abs(h) % tints.length] ?? tints[0];
  const showImage = Boolean(src) && failed !== src;
  return (
    <View
      accessibilityRole="image"
      accessibilityLabel={name}
      style={[{ width: d, height: d, borderRadius: d / 2, overflow: "hidden", backgroundColor: bg, alignItems: "center", justifyContent: "center" }, ring ? { borderWidth: 2, borderColor: c.surface } : null, style]}
    >
      {showImage && src ? (
        <Image source={{ uri: src }} style={{ width: "100%", height: "100%" }} contentFit="cover" transition={theme.motion.duration.fast} onError={() => setFailed(src)} />
      ) : (
        <Text weight={600} maxFontSizeMultiplier={1} style={{ color: fg, fontSize: Math.round(d * 0.4), lineHeight: Math.round(d * 0.5) }}>
          {initials(name)}
        </Text>
      )}
    </View>
  );
}

export interface AvatarGroupProps {
  people: { name: string; src?: string | null }[];
  max?: number;
  size?: AvatarSize;
  style?: StyleProp<ViewStyle>;
}

/** Overlapping avatars with +N. */
export function AvatarGroup({ people, max = 4, size = "sm", style }: AvatarGroupProps) {
  const theme = useTheme();
  const d = SIZES[size];
  const shown = people.slice(0, max);
  const extra = people.length - shown.length;
  return (
    <View style={[{ flexDirection: "row", alignItems: "center" }, style]}>
      {shown.map((p, i) => (
        <Avatar key={`${p.name}-${i}`} name={p.name} src={p.src ?? null} size={size} ring style={i > 0 ? { marginLeft: -d / 4 } : undefined} />
      ))}
      {extra > 0 ? (
        <View style={{ marginLeft: -d / 4, width: d, height: d, borderRadius: d / 2, borderWidth: 2, borderColor: theme.colors.surface, backgroundColor: theme.colors.sunken, alignItems: "center", justifyContent: "center" }}>
          <Text role="caption" weight={600} tone="muted" maxFontSizeMultiplier={1}>
            {`+${extra}`}
          </Text>
        </View>
      ) : null}
    </View>
  );
}
