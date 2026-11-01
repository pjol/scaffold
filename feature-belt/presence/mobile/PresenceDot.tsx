import { isPresenceDND, presenceLabel, useUserPresence, type Presence } from "@scaffoldapp/shared";
import { Avatar, useTheme, type AvatarSize } from "@scaffoldapp/ui-native";
import { View } from "react-native";

export interface PresenceDotProps {
  presence: Presence | null | undefined;
  /** `sm` 8 · `md` 12. */
  size?: "sm" | "md";
  /** Draw offline as a hollow dot instead of nothing. */
  showOffline?: boolean;
  /** A ring in the surface colour, for a dot over an avatar. */
  ring?: boolean;
}

/** Online (filled), away (hollow), do not disturb (filled with a bar), offline (hidden unless `showOffline`). */
export function PresenceDot({ presence, size = "sm", showOffline = false, ring = false }: PresenceDotProps) {
  const theme = useTheme();
  const c = theme.colors;
  const status = presence?.status ?? "offline";
  const dnd = isPresenceDND(presence);
  if (status === "offline" && !showOffline && !dnd) return null;
  const d = size === "md" ? theme.space[3] : theme.space[2];
  const filled = dnd || status === "online";
  return (
    <View
      accessible
      accessibilityRole="image"
      accessibilityLabel={presenceLabel(presence)}
      style={{
        width: d + (ring ? 4 : 0),
        height: d + (ring ? 4 : 0),
        borderRadius: theme.radius.pill,
        backgroundColor: ring ? c.surface : "transparent",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      <View
        style={{
          width: d,
          height: d,
          borderRadius: theme.radius.pill,
          backgroundColor: dnd ? c.danger : status === "online" ? c.success : c.surface,
          borderWidth: filled ? 0 : 2,
          borderColor: status === "away" ? c.textMuted : c.textSubtle,
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        {dnd ? <View style={{ width: d / 2, height: 1, backgroundColor: c.dangerFg }} /> : null}
      </View>
    </View>
  );
}

export function UserPresenceDot({ userId, ...props }: Omit<PresenceDotProps, "presence"> & { userId: string | null | undefined }) {
  return <PresenceDot presence={useUserPresence(userId)} {...props} />;
}

/** Avatar with the person's presence dot at the bottom right. */
export function PresenceAvatar({ userId, name, src, size = "md", showOffline }: { userId: string | null | undefined; name: string; src?: string | null; size?: AvatarSize; showOffline?: boolean }) {
  const presence = useUserPresence(userId);
  const big = size === "lg" || size === "xl" || size === "2xl";
  return (
    <View>
      <Avatar name={name} src={src ?? null} size={size} />
      <View style={{ position: "absolute", right: -2, bottom: -2 }}>
        <PresenceDot presence={presence} size={big ? "md" : "sm"} ring showOffline={showOffline} />
      </View>
    </View>
  );
}
