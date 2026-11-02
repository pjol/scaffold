import { ArrowUpRight, X } from "lucide-react-native";
import type { ReactNode } from "react";
import { Pressable, View, type StyleProp, type ViewStyle } from "react-native";
import { InfoTip } from "../components/InfoTip";
import { Text } from "../components/Text";
import { useTheme } from "../theme/ThemeProvider";
import { ProviderIcon } from "./ProviderIcon";

/** A capability the person could connect (shared `ConnectionOffer`). */
export interface ConnectPromptOffer {
  capability: string;
  provider: string;
  label: string;
  connect_url: string;
}

export interface ConnectPromptProps {
  /** From `GET /v1/<feature>/sources` → `offers`. Nothing renders when empty. */
  offers: ConnectPromptOffer[] | undefined;
  /** Opens an offer: navigate to `settings/connections?capability=…` (the connect_url without its leading slash). */
  onConnect: (offer: ConnectPromptOffer) => void;
  /** @default "Connect your accounts" */
  title?: string;
  info?: ReactNode;
  /** Shows a close button; the feature remembers the choice. */
  onDismiss?: () => void;
  style?: StyleProp<ViewStyle>;
}

/** A quiet offer to bring connected content into this view: one row per capability, never a nag. */
export function ConnectPrompt({ offers, onConnect, title = "Connect your accounts", info, onDismiss, style }: ConnectPromptProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  if (!offers || offers.length === 0) return null;
  return (
    <View style={[{ borderWidth: 1, borderStyle: "dashed", borderColor: c.borderStrong, borderRadius: theme.radius.lg, padding: s[3], gap: s[1] }, style]}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], minHeight: 32, paddingHorizontal: s[1] }}>
        <Text role="label" weight={600} tone="muted" style={{ flexShrink: 1 }} accessibilityRole="header">
          {title}
        </Text>
        {info ? <InfoTip content={info} /> : null}
        <View style={{ flex: 1 }} />
        {onDismiss ? (
          <Pressable onPress={onDismiss} accessibilityRole="button" accessibilityLabel="Dismiss" hitSlop={8} style={{ padding: s[1] }}>
            <X size={16} color={c.textSubtle} />
          </Pressable>
        ) : null}
      </View>
      {offers.map((o) => (
        <Pressable
          key={o.capability}
          onPress={() => onConnect(o)}
          accessibilityRole="link"
          accessibilityLabel={`Connect ${o.label}`}
          style={({ pressed }) => ({
            flexDirection: "row",
            alignItems: "center",
            gap: s[3],
            minHeight: theme.tap.base,
            paddingHorizontal: s[2],
            borderRadius: theme.radius.md,
            backgroundColor: pressed ? c.sunken : "transparent",
          })}
        >
          <View style={{ width: 32, height: 32, borderRadius: theme.radius.md, backgroundColor: c.sunken, alignItems: "center", justifyContent: "center" }}>
            <ProviderIcon provider={o.provider} size={16} />
          </View>
          <Text role="body" weight={500} numberOfLines={1} style={{ flex: 1 }}>
            {o.label}
          </Text>
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
            <Text role="label" weight={600} tone="primary">
              Connect
            </Text>
            <ArrowUpRight size={16} color={c.primaryStrong} />
          </View>
        </Pressable>
      ))}
    </View>
  );
}
