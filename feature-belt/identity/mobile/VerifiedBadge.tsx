import { useIdentityUserBadge, type IdentityHeldBadge } from "@scaffoldapp/shared";
import { useTheme } from "@scaffoldapp/ui-native";
import { BadgeCheck } from "lucide-react-native";
import { View } from "react-native";
import { IdentityBadgeMark, identityBadgeName } from "./BadgeMark";

/**
 * The mark next to a person's name: their highest verification badge (official, org, notable
 * or paid, each with its own mark), else the ID-verified check. Pass `badge` and/or `verified`
 * when you already know them, or `userId` to read the public badge. Renders nothing when there's
 * nothing to show.
 */
export function VerifiedBadge({ userId, verified, badge, size = 18 }: { userId?: string | null; verified?: boolean; badge?: IdentityHeldBadge | null; size?: number }) {
  const theme = useTheme();
  const known = verified !== undefined || badge !== undefined;
  const pub = useIdentityUserBadge(known ? null : userId);
  const isVerified = verified ?? pub.data?.verified ?? false;
  const held = badge !== undefined ? badge : (pub.data?.badge ?? null);
  if (!held && !isVerified) return null;
  return (
    <View accessible accessibilityRole="image" accessibilityLabel={held ? identityBadgeName(held) : "Verified identity"}>
      {held ? <IdentityBadgeMark kind={held.kind} size={size} /> : <BadgeCheck size={size} color={theme.colors.primaryStrong} strokeWidth={2.25} />}
    </View>
  );
}
