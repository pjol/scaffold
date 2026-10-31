import { errorMessage, shortAddress, useUnlinkWallet, useUpdateWallet, useWallets, useWalletsConfig, walletsConfigured, type Wallet } from "@scaffoldapp/shared";
import { Badge, Body, Button, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, Input, ListGroup, ListRow, Screen, Sheet, SkeletonRow, haptic, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack } from "expo-router";
import { Pencil, Share2, Star, Unlink, Wallet as WalletIcon } from "lucide-react-native";
import { useRef, useState } from "react";
import { Share, View } from "react-native";
import { LinkOnWebButton, WalletsNotConfigured } from "./components";

type Action = "rename" | "primary" | "share" | "unlink";

function walletTitle(w: Wallet): string {
  return w.label ? `${w.label} · ${shortAddress(w.address)}` : shortAddress(w.address);
}

/** Linked wallets: rename, make primary, share the address, unlink. Linking opens the web app. */
export default function ManageWalletsScreen() {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const config = useWalletsConfig();
  const wallets = useWallets();
  const update = useUpdateWallet();
  const unlink = useUnlinkWallet();
  const [selected, setSelected] = useState<Wallet | null>(null);
  const [renaming, setRenaming] = useState<Wallet | null>(null);
  const [unlinking, setUnlinking] = useState<Wallet | null>(null);
  const [label, setLabel] = useState("");
  const pending = useRef<{ action: Action; wallet: Wallet } | null>(null);
  const list = wallets.data ?? [];
  const linkUrl = config.data?.link_url ?? "";

  const run = async (action: Action, wallet: Wallet) => {
    if (action === "rename") {
      setLabel(wallet.label);
      setRenaming(wallet);
    } else if (action === "unlink") {
      setUnlinking(wallet);
    } else if (action === "share") {
      await Share.share({ message: wallet.address });
    } else {
      update.mutate(
        { id: wallet.id, is_primary: true },
        { onSuccess: () => haptic.success(), onError: (e) => toast.error(errorMessage(e, "Couldn't change the primary wallet. Try again.")) },
      );
    }
  };

  const choose = (action: Action) => {
    if (selected) pending.current = { action, wallet: selected };
    setSelected(null);
  };

  let body;
  if (config.isPending || wallets.isPending) {
    body = (
      <View accessibilityLabel="Loading" accessibilityRole="progressbar">
        <SkeletonRow />
        <SkeletonRow />
      </View>
    );
  } else if (config.isError || wallets.isError) {
    const failed = config.isError ? config : wallets;
    body = <ErrorState title="Couldn't load your wallets" info={failed.error?.message} onRetry={() => void failed.refetch()} retrying={failed.isRefetching} />;
  } else if (!walletsConfigured(config.data) && list.length === 0) {
    body = <WalletsNotConfigured />;
  } else if (list.length === 0) {
    body = <EmptyState icon={WalletIcon} title="No wallets linked" action={<LinkOnWebButton url={linkUrl} />} />;
  } else {
    body = (
      <ListGroup title="Linked wallets" info="Linking signs a message that proves you control the address. It never moves funds or shares keys.">
        {list.map((w) => (
          <ListRow
            key={w.id}
            title={walletTitle(w)}
            leading={<WalletIcon size={22} color={c.textMuted} strokeWidth={2} />}
            trailing={
              w.is_primary ? (
                <Badge size="sm" tone="primary">
                  Primary
                </Badge>
              ) : w.kind === "embedded" ? (
                <Badge size="sm">Embedded</Badge>
              ) : undefined
            }
            onPress={() => setSelected(w)}
          />
        ))}
      </ListGroup>
    );
  }

  const canLink = walletsConfigured(config.data) && list.length > 0 && list.length < (config.data?.max_wallets ?? 0);

  return (
    <Screen edges={["bottom"]} {...(canLink ? { footer: <LinkOnWebButton url={linkUrl} variant="secondary" /> } : {})}>
      <Stack.Screen options={{ title: "Wallets" }} />
      {body}
      <Sheet
        open={selected !== null}
        onClose={() => setSelected(null)}
        onDismissed={() => {
          const next = pending.current;
          pending.current = null;
          if (next) void run(next.action, next.wallet);
        }}
        title={selected ? walletTitle(selected) : "Wallet"}
        snapPoints={[selected?.is_primary ? 300 : 360]}
      >
        <View style={{ marginHorizontal: -s[4] }}>
          <ListRow title="Rename" leading={<Pencil size={22} color={c.textMuted} strokeWidth={2} />} chevron={false} onPress={() => choose("rename")} />
          {selected && !selected.is_primary ? (
            <ListRow title="Make primary" leading={<Star size={22} color={c.textMuted} strokeWidth={2} />} chevron={false} onPress={() => choose("primary")} />
          ) : null}
          <ListRow title="Share address" leading={<Share2 size={22} color={c.textMuted} strokeWidth={2} />} chevron={false} onPress={() => choose("share")} />
          <ListRow title="Unlink" destructive leading={<Unlink size={22} color={c.dangerStrong} strokeWidth={2} />} chevron={false} onPress={() => choose("unlink")} />
        </View>
      </Sheet>
      <Dialog
        open={renaming !== null}
        onClose={() => setRenaming(null)}
        title="Rename wallet"
        footer={
          <Button
            fullWidth
            onPress={() => {
              if (!renaming) return;
              update.mutate(
                { id: renaming.id, label: label.trim() },
                { onSuccess: () => toast.success("Saved"), onError: (e) => toast.error(errorMessage(e, "Couldn't rename the wallet. Try again.")) },
              );
              setRenaming(null);
            }}
          >
            Save
          </Button>
        }
      >
        <Field label="Label" info="Leave it empty to show the address.">
          <Input value={label} onChangeText={setLabel} maxLength={40} autoFocus returnKeyType="done" />
        </Field>
      </Dialog>
      <ConfirmDialog
        open={unlinking !== null}
        onClose={() => setUnlinking(null)}
        title={unlinking ? `Unlink ${unlinking.label || shortAddress(unlinking.address)}?` : "Unlink wallet?"}
        confirmLabel="Unlink"
        tone="danger"
        onConfirm={async () => {
          if (!unlinking) return;
          await unlink.mutateAsync(unlinking.id);
          toast.success("Wallet unlinked");
        }}
        errorText={(e) => errorMessage(e, "Couldn't unlink the wallet. Try again.")}
      >
        <Body tone="muted">It leaves your balances and activity. You can link it again later.</Body>
      </ConfirmDialog>
    </Screen>
  );
}
