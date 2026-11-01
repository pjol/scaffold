import { formatUnits, shortAddress, type WalletTransfer } from "@scaffoldapp/shared";
import { Button, EmptyState, ListRow, Text, useTheme } from "@scaffoldapp/ui-native";
import { ArrowDownLeft, ArrowLeftRight, ArrowUpRight, ExternalLink, WalletMinimal } from "lucide-react-native";
import { Linking, View } from "react-native";

/** No chains configured on the server: the honest state. */
export function WalletsNotConfigured() {
  return (
    <EmptyState
      icon={WalletMinimal}
      title="Wallets aren't set up"
      info="This app has no blockchain networks configured yet, so there's nothing to show. Contact support if you expected to see your wallets here."
    />
  );
}

/**
 * Linking a wallet happens on the web until the app ships WalletConnect or Privy
 * (FEATURE.md, Stitching). Opens the web page where wallets are linked.
 */
export function LinkOnWebButton({ url, variant = "primary" }: { url: string; variant?: "primary" | "secondary" }) {
  return (
    <Button variant={variant} fullWidth rightIcon={ExternalLink} disabled={!url} onPress={() => void Linking.openURL(url)}>
      Link a wallet on the web
    </Button>
  );
}

/** "ETH", "USDC" in a round tile. */
export function AssetMark({ symbol }: { symbol: string }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      style={{ minWidth: s[10], height: s[10], paddingHorizontal: s[1], borderRadius: s[5], backgroundColor: theme.colors.primarySoft, alignItems: "center", justifyContent: "center" }}
    >
      <Text role="caption" weight={600} tone="primary" maxFontSizeMultiplier={1.5}>
        {symbol.slice(0, 4)}
      </Text>
    </View>
  );
}

/** One transfer row: direction, counterparty and signed amount; opens the explorer when there is one. */
export function TransferRow({ transfer }: { transfer: WalletTransfer }) {
  const theme = useTheme();
  const c = theme.colors;
  const kind = {
    in: { Icon: ArrowDownLeft, color: c.successStrong, bg: c.successSoft, sign: "+", tone: "success" as const },
    out: { Icon: ArrowUpRight, color: c.text, bg: c.sunken, sign: "−", tone: "default" as const },
    self: { Icon: ArrowLeftRight, color: c.textMuted, bg: c.sunken, sign: "", tone: "muted" as const },
  }[transfer.direction];
  const title =
    transfer.direction === "in" ? `From ${shortAddress(transfer.from)}` : transfer.direction === "out" ? `To ${shortAddress(transfer.to)}` : "Between your wallets";
  const amount = `${kind.sign}${formatUnits(transfer.amount, transfer.decimals)} ${transfer.symbol}`;
  const s = theme.space;
  const url = transfer.explorer_url;
  return (
    <ListRow
      title={title}
      leading={
        <View style={{ width: s[10], height: s[10], borderRadius: theme.radius.md, backgroundColor: kind.bg, alignItems: "center", justifyContent: "center" }}>
          <kind.Icon size={20} color={kind.color} strokeWidth={2} />
        </View>
      }
      trailing={
        <Text role="body" weight={600} tone={kind.tone} tabular>
          {amount}
        </Text>
      }
      chevron={false}
      {...(url ? { onPress: () => void Linking.openURL(url), accessibilityHint: "Opens the block explorer" } : {})}
    />
  );
}
