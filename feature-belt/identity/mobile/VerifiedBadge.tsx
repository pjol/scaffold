import { useIdentityBadge } from "@scaffoldapp/shared";
import { useTheme } from "@scaffoldapp/ui-native";
import { BadgeCheck } from "lucide-react-native";
import { View } from "react-native";

/**
 * The verified mark next to a person's name. Pass `verified` when you already know it, or
 * `userId` to read the public badge. Renders nothing for unverified users.
 */
export function VerifiedBadge({ userId, verified, size = 18 }: { userId?: string | null; verified?: boolean; size?: number }) {
  const theme = useTheme();
  const badge = useIdentityBadge(verified === undefined ? userId : null);
  if (!(verified ?? badge.data)) return null;
  return (
    <View accessible accessibilityRole="image" accessibilityLabel="Verified identity">
      <BadgeCheck size={size} color={theme.colors.primaryStrong} strokeWidth={2.25} />
    </View>
  );
}
