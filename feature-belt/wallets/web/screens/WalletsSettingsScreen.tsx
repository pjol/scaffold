"use client";

import {
  errorMessage,
  explorerAddressUrl,
  findChain,
  shortAddress,
  useUnlinkWallet,
  useUpdateWallet,
  useWallets,
  useWalletsConfig,
  walletsConfigured,
  type Wallet,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, Dialog, Divider, EmptyState, ErrorState, Field, IconButton, Input, ListRow, Menu, MenuItem, MenuSeparator, toast } from "@scaffoldapp/ui-web";
import { Copy, ExternalLink, MoreHorizontal, Pencil, Plus, Star, Unlink, Wallet as WalletIcon } from "lucide-react";
import { Fragment, useEffect, useState, type FormEvent } from "react";
import { Screen } from "@/components/shell/PageHeader";
import { LinkWalletDialog } from "../components/LinkWalletDialog";
import { RowsSkeleton, WalletsNotConfigured } from "../components/states";

function RenameDialog({ wallet, onClose }: { wallet: Wallet | null; onClose: () => void }) {
  const update = useUpdateWallet();
  const [label, setLabel] = useState("");
  useEffect(() => setLabel(wallet?.label ?? ""), [wallet]);
  const save = (e: FormEvent) => {
    e.preventDefault();
    if (!wallet) return;
    update.mutate(
      { id: wallet.id, label: label.trim() },
      { onSuccess: () => toast.success("Saved"), onError: (err) => toast.error(errorMessage(err, "Couldn't rename the wallet. Try again.")) },
    );
    onClose();
  };
  return (
    <Dialog
      open={wallet !== null}
      onOpenChange={(open) => !open && onClose()}
      title="Rename wallet"
      size="sm"
      footer={
        <Button type="submit" form="rename-wallet-form" size="auto">
          Save
        </Button>
      }
    >
      <form id="rename-wallet-form" onSubmit={save} noValidate>
        <Field label="Label" info="Leave it empty to show the address.">
          <Input value={label} onChange={(e) => setLabel(e.target.value)} maxLength={40} autoFocus autoComplete="off" />
        </Field>
      </form>
    </Dialog>
  );
}

function WalletRow({ wallet, explorer, onRename, onUnlink }: { wallet: Wallet; explorer: string | null; onRename: () => void; onUnlink: () => void }) {
  const update = useUpdateWallet();
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(wallet.address);
      toast.success("Address copied");
    } catch {
      toast.error("Couldn't copy. Select the address and copy it instead.");
    }
  };
  const makePrimary = () =>
    update.mutate({ id: wallet.id, is_primary: true }, { onError: (err) => toast.error(errorMessage(err, "Couldn't change the primary wallet. Try again.")) });

  return (
    <ListRow
      title={
        <span className="flex min-w-0 items-baseline gap-2">
          {wallet.label && <span className="min-w-0 truncate">{wallet.label}</span>}
          <span className={wallet.label ? "hidden shrink-0 type-label tabular-nums text-fg-muted sm:inline" : "truncate tabular-nums"} title={wallet.address}>
            {shortAddress(wallet.address)}
          </span>
        </span>
      }
      leading={
        <span className="flex size-10 items-center justify-center rounded-md bg-sunken text-fg">
          <WalletIcon className="size-5" aria-hidden />
        </span>
      }
      trailing={
        <>
          {wallet.is_primary && (
            <Badge size="sm" tone="primary">
              Primary
            </Badge>
          )}
          {wallet.kind === "embedded" && (
            <Badge size="sm" tone="neutral" className="hidden sm:inline-flex">
              Embedded
            </Badge>
          )}
          <Menu trigger={<IconButton label={`Actions for ${wallet.label || shortAddress(wallet.address)}`} icon={<MoreHorizontal />} variant="ghost" />} align="end">
            <MenuItem icon={<Copy />} onSelect={() => void copy()}>
              Copy address
            </MenuItem>
            <MenuItem icon={<Pencil />} onSelect={onRename}>
              Rename
            </MenuItem>
            {!wallet.is_primary && (
              <MenuItem icon={<Star />} onSelect={makePrimary}>
                Make primary
              </MenuItem>
            )}
            {explorer && (
              <MenuItem asChild>
                <a href={explorer} target="_blank" rel="noopener noreferrer">
                  <ExternalLink aria-hidden />
                  View on explorer
                </a>
              </MenuItem>
            )}
            <MenuSeparator />
            <MenuItem icon={<Unlink />} destructive onSelect={onUnlink}>
              Unlink
            </MenuItem>
          </Menu>
        </>
      }
    />
  );
}

/** Settings → Wallets: linked wallets (rename, primary, unlink) and the link flow. */
export function WalletsSettingsScreen() {
  const config = useWalletsConfig();
  const wallets = useWallets();
  const unlink = useUnlinkWallet();
  const [linking, setLinking] = useState(false);
  const [renaming, setRenaming] = useState<Wallet | null>(null);
  const [unlinking, setUnlinking] = useState<Wallet | null>(null);
  const list = wallets.data ?? [];
  const configured = walletsConfigured(config.data);
  const max = config.data?.max_wallets ?? 0;
  const atLimit = max > 0 && list.length >= max;
  const defaultChain = config.data?.default_chain_id ? findChain(config.data, config.data.default_chain_id) : config.data?.chains[0];

  const linkButton =
    configured && list.length > 0 ? (
      <Button size="auto" variant="secondary" leftIcon={<Plus />} disabled={atLimit} onClick={() => setLinking(true)}>
        Link a wallet
      </Button>
    ) : undefined;

  let body;
  if (config.isPending || wallets.isPending) {
    body = <RowsSkeleton rows={2} label="Loading wallets" />;
  } else if (config.isError || wallets.isError) {
    const failed = config.isError ? config : wallets;
    body = (
      <Card padding="compact">
        <ErrorState compact title="Couldn't load your wallets" info={failed.error?.message} onRetry={() => void failed.refetch()} retrying={failed.isFetching} />
      </Card>
    );
  } else if (!configured && list.length === 0) {
    body = <WalletsNotConfigured />;
  } else if (list.length === 0) {
    body = (
      <Card padding="compact">
        <EmptyState
          icon={<WalletIcon />}
          title="No wallets linked"
          action={
            <Button size="auto" leftIcon={<Plus />} onClick={() => setLinking(true)}>
              Link a wallet
            </Button>
          }
        />
      </Card>
    );
  } else {
    body = (
      <Card padding="none" className="p-1">
        {list.map((w, i) => (
          <Fragment key={w.id}>
            {i > 0 && <Divider className="mx-4" />}
            <WalletRow wallet={w} explorer={explorerAddressUrl(defaultChain, w.address)} onRename={() => setRenaming(w)} onUnlink={() => setUnlinking(w)} />
          </Fragment>
        ))}
      </Card>
    );
  }

  return (
    <Screen
      title="Wallets"
      info={`Link up to ${max || 10} wallets. Linking signs a message that proves you control the address; it never moves funds or shares keys.`}
      {...(linkButton ? { action: linkButton } : {})}
    >
      {body}
      <LinkWalletDialog open={linking} onOpenChange={setLinking} />
      <RenameDialog wallet={renaming} onClose={() => setRenaming(null)} />
      <ConfirmDialog
        open={unlinking !== null}
        onOpenChange={(open) => !open && setUnlinking(null)}
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
        It leaves your balances and activity. You can link it again later.
      </ConfirmDialog>
    </Screen>
  );
}
