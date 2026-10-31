import type { ChatConversation } from "@scaffoldapp/shared";
import { Avatar, useTheme, type AvatarSize } from "@scaffoldapp/ui-native";
import { Users } from "lucide-react-native";
import { View } from "react-native";

const BOX: Record<"sm" | "md" | "lg" | "xl", number> = { sm: 32, md: 40, lg: 48, xl: 64 };

/** The other person, the group photo, or a group icon. */
export function ConversationAvatar({ conversation, size = "lg" }: { conversation: Pick<ChatConversation, "kind" | "title" | "avatar_url">; size?: Extract<AvatarSize, "sm" | "md" | "lg" | "xl"> }) {
  const theme = useTheme();
  if (conversation.kind === "group" && !conversation.avatar_url) {
    const box = BOX[size];
    return (
      <View
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
        style={{ width: box, height: box, borderRadius: box / 2, backgroundColor: theme.colors.primarySoft, alignItems: "center", justifyContent: "center" }}
      >
        <Users color={theme.colors.primaryStrong} size={box / 2} strokeWidth={2} />
      </View>
    );
  }
  return <Avatar name={conversation.title} src={conversation.avatar_url} size={size} />;
}
