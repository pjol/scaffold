import { Lock, Plus } from "lucide-react-native";
import type { ReactNode } from "react";
import { Pressable, View, type StyleProp, type ViewStyle } from "react-native";
import { Skeleton } from "../components/Skeleton";
import { Text } from "../components/Text";
import { useTheme } from "../theme/ThemeProvider";
import { sourceProductName, type SourceStatus } from "./marks";
import { ProviderIcon } from "./ProviderIcon";

/** One connected account in a feature's navigation. */
export interface ConnectedSectionItem {
  id: string;
  provider: string;
  product?: string | null;
  label?: string;
  account?: string | null;
  status?: SourceStatus;
  /** Where "Reconnect" goes when status is needs_reauth (an app path). */
  fixUrl?: string | null;
}

export interface ConnectedSectionProps {
  items: ConnectedSectionItem[] | undefined;
  /** @default "Connected" */
  title?: string;
  activeId?: string | null;
  onSelect: (id: string) => void;
  /** Reconnect: navigate to the item's fixUrl. */
  onFix?: (item: ConnectedSectionItem) => void;
  /** The header's +: open Settings → Connections for this feature's capability. */
  onAdd?: () => void;
  action?: ReactNode;
  loading?: boolean;
  style?: StyleProp<ViewStyle>;
}

/** Builds ConnectedSection items from a feature's sources (shared `FeatureSource`); app sources are skipped. */
export function connectedSectionItems(
  sources: Array<{ id: string; type: string; provider?: string; product?: string; account?: string; label: string; status?: SourceStatus; fix_url?: string }> | undefined,
): ConnectedSectionItem[] {
  return (sources ?? [])
    .filter((x) => x.type === "connected" && x.provider)
    .map((x) => ({
      id: x.id,
      provider: x.provider!,
      ...(x.product ? { product: x.product } : {}),
      label: x.label,
      ...(x.account ? { account: x.account } : {}),
      ...(x.status ? { status: x.status } : {}),
      ...(x.fix_url ? { fixUrl: x.fix_url } : {}),
    }));
}

/** The "Connected" section: one row per connected account for this feature, with status and Reconnect. */
export function ConnectedSection({ items, title = "Connected", activeId, onSelect, onFix, onAdd, action, loading, style }: ConnectedSectionProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const list = items ?? [];
  if (!loading && list.length === 0) return null;
  return (
    <View style={[{ gap: s[0.5] }, style]}>
      <View style={{ flexDirection: "row", alignItems: "center", minHeight: 32, paddingHorizontal: s[4] }}>
        <Text role="caption" weight={600} tone="muted" accessibilityRole="header" style={{ flex: 1 }}>
          {title}
        </Text>
        {action ??
          (onAdd ? (
            <Pressable onPress={onAdd} accessibilityRole="button" accessibilityLabel="Connect an account" hitSlop={12} style={{ padding: s[1] }}>
              <Plus size={16} color={c.textSubtle} />
            </Pressable>
          ) : null)}
      </View>
      {loading && list.length === 0 ? (
        <View style={{ gap: s[2], paddingHorizontal: s[4] }}>
          <Skeleton height={24} />
          <Skeleton height={24} width="70%" />
        </View>
      ) : (
        list.map((it) => {
          const name = it.label || sourceProductName(it.provider, it.product);
          const blocked = it.status === "blocked";
          const reauth = it.status === "needs_reauth";
          const statusText = reauth ? ", needs reconnecting" : blocked ? ", blocked by your workspace" : "";
          return (
            <View key={it.id} style={{ flexDirection: "row", alignItems: "center", paddingHorizontal: s[2] }}>
              <Pressable
                disabled={blocked}
                onPress={() => onSelect(it.id)}
                accessibilityRole="button"
                accessibilityState={{ selected: it.id === activeId, disabled: blocked }}
                accessibilityLabel={`${name}${it.account && !name.endsWith(it.account) ? ` · ${it.account}` : ""}${statusText}`}
                style={({ pressed }) => ({
                  flex: 1,
                  flexDirection: "row",
                  alignItems: "center",
                  gap: s[3],
                  minHeight: theme.tap.base,
                  paddingHorizontal: s[2],
                  borderRadius: theme.radius.md,
                  backgroundColor: it.id === activeId || pressed ? c.sunken : "transparent",
                })}
              >
                <ProviderIcon provider={it.provider} product={it.product} size={18} {...(blocked ? { color: c.textSubtle } : {})} />
                <Text role="body" numberOfLines={1} tone={blocked ? "subtle" : "default"} style={{ flex: 1 }}>
                  {name}
                  {it.account && !name.endsWith(it.account) ? <Text role="body" tone="muted">{` · ${it.account}`}</Text> : null}
                </Text>
                {blocked ? <Lock size={16} color={c.textSubtle} /> : null}
              </Pressable>
              {reauth && onFix ? (
                <Pressable onPress={() => onFix(it)} accessibilityRole="link" hitSlop={8} style={{ paddingHorizontal: s[2], paddingVertical: s[2] }}>
                  <Text role="label" weight={600} tone="warning">
                    Reconnect
                  </Text>
                </Pressable>
              ) : null}
            </View>
          );
        })
      )}
    </View>
  );
}
