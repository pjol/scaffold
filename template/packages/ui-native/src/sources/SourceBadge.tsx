import { View, type StyleProp, type ViewStyle } from "react-native";
import { Text } from "../components/Text";
import { useTheme } from "../theme/ThemeProvider";
import { sourceAccessibleLabel, sourceProductName } from "./marks";
import { ProviderIcon } from "./ProviderIcon";

/** A row's source as features send it (shared `SourceRef`); app rows (`{ type: "app" }`, null) render nothing. */
export interface SourceBadgeSource {
  type?: string;
  provider?: string;
  product?: string | null;
  account?: string | null;
}

export interface SourceBadgeProps {
  /** The row's `source`. Nothing renders for the app's own content. */
  source: SourceBadgeSource | null | undefined;
  /** Name override. @default the product's name */
  label?: string;
  /** Show the account after the name. It is always in the accessible label. */
  showAccount?: boolean;
  /** Icon only, for dense rows (the accessible label still names the source). */
  iconOnly?: boolean;
  style?: StyleProp<ViewStyle>;
}

/** Marks connected content: provider icon + product name. The app's own content carries no badge. */
export function SourceBadge({ source, label, showAccount, iconOnly, style }: SourceBadgeProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  if (!source || source.type === "app" || !source.provider) return null;
  const name = label || sourceProductName(source.provider, source.product);
  const a11yLabel = sourceAccessibleLabel(source.provider, source.product, source.account, name);
  if (iconOnly) {
    return (
      <View accessible accessibilityRole="image" accessibilityLabel={a11yLabel} style={[{ padding: s[1] }, style]}>
        <ProviderIcon provider={source.provider} product={source.product} size={14} color={c.textMuted} />
      </View>
    );
  }
  return (
    <View
      accessible
      accessibilityLabel={a11yLabel}
      style={[
        {
          flexDirection: "row",
          alignItems: "center",
          alignSelf: "flex-start",
          maxWidth: "100%",
          gap: s[1],
          paddingHorizontal: s[1.5],
          paddingVertical: s[0.5],
          borderRadius: theme.radius.sm,
          borderWidth: 1,
          borderColor: c.border,
          backgroundColor: c.surface,
        },
        style,
      ]}
    >
      <ProviderIcon provider={source.provider} product={source.product} size={14} />
      <Text role="caption" weight={500} tone="muted" numberOfLines={1} maxFontSizeMultiplier={1.6} style={{ flexShrink: 1 }}>
        {showAccount && source.account ? `${name} · ${source.account}` : name}
      </Text>
    </View>
  );
}
