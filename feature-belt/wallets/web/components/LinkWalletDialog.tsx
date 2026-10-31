"use client";

import { errorMessage, shortAddress, useLinkWallet, useWallets } from "@scaffoldapp/shared";
import { Banner, Button, Dialog, Field, Input, Select, toast } from "@scaffoldapp/ui-web";
import { PlugZap } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { injectedProvider, personalSign, requestAccounts, walletChainId, walletErrorMessage } from "../ethereum";

/**
 * Link a browser wallet: connect (eth_requestAccounts) → pick the account → sign the
 * server's Sign-In with Ethereum message (personal_sign) → the server verifies and links.
 */
export function LinkWalletDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const wallets = useWallets();
  const link = useLinkWallet();
  const [hasProvider, setHasProvider] = useState(true);
  const [accounts, setAccounts] = useState<string[]>([]);
  const [address, setAddress] = useState("");
  const [label, setLabel] = useState("");
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setHasProvider(injectedProvider() !== null);
    setAccounts([]);
    setAddress("");
    setLabel("");
    setError(null);
    link.reset();
    // Reset once per opening.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const linked = new Set((wallets.data ?? []).map((w) => w.address.toLowerCase()));

  const connect = async () => {
    setError(null);
    setConnecting(true);
    try {
      const list = await requestAccounts();
      if (list.length === 0) {
        setError("Your wallet shared no accounts. Open your wallet and try again.");
        return;
      }
      setAccounts(list);
      setAddress(list.find((a) => !linked.has(a.toLowerCase())) ?? list[0] ?? "");
    } catch (e) {
      setError(walletErrorMessage(e) ?? errorMessage(e, "Couldn't connect to your wallet. Try again."));
    } finally {
      setConnecting(false);
    }
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!address) return;
    setError(null);
    try {
      const chainId = await walletChainId();
      await link.mutateAsync({
        address,
        label: label.trim(),
        ...(chainId ? { chainId } : {}),
        sign: (message) => personalSign(message, address),
      });
      toast.success("Wallet linked");
      onOpenChange(false);
    } catch (err) {
      setError(walletErrorMessage(err) ?? errorMessage(err, "Couldn't link this wallet. Try again."));
    }
  };

  const alreadyLinked = address !== "" && linked.has(address.toLowerCase());
  const busy = link.isPending;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !busy && onOpenChange(next)}
      title="Link a wallet"
      info="Your wallet signs a message that proves you control the address. Signing is free and moves no funds."
      footer={
        !hasProvider ? (
          <Button key="close" variant="secondary" size="auto" onClick={() => onOpenChange(false)}>
            Close
          </Button>
        ) : accounts.length === 0 ? (
          <Button key="connect" size="auto" leftIcon={<PlugZap />} loading={connecting} onClick={() => void connect()}>
            Connect wallet
          </Button>
        ) : (
          <Button key="submit" type="submit" form="link-wallet-form" size="auto" loading={busy} disabled={!address || alreadyLinked}>
            {busy ? "Check your wallet" : "Sign and link"}
          </Button>
        )
      }
    >
      <div className="flex flex-col gap-4">
        {!hasProvider ? (
          <Banner tone="warning">No browser wallet found. Install a wallet extension such as MetaMask or Rabby, then reload this page.</Banner>
        ) : accounts.length === 0 ? (
          <p className="type-body text-fg-muted">Connect the wallet you want to link.</p>
        ) : (
          <form id="link-wallet-form" className="flex flex-col gap-4" onSubmit={(e) => void submit(e)} noValidate>
            {accounts.length > 1 ? (
              <Field label="Account" error={alreadyLinked ? "This wallet is already linked." : undefined}>
                <Select
                  value={address}
                  onValueChange={setAddress}
                  options={accounts.map((a) => ({ value: a, label: `${shortAddress(a, 6)}${linked.has(a.toLowerCase()) ? " (linked)" : ""}` }))}
                />
              </Field>
            ) : (
              <Field label="Account" error={alreadyLinked ? "This wallet is already linked." : undefined}>
                <Input value={address} readOnly className="tabular-nums text-fg-muted" />
              </Field>
            )}
            <Field label="Label" info="A name only you see, like Savings or Ledger.">
              <Input value={label} onChange={(e) => setLabel(e.target.value)} maxLength={40} placeholder="Optional" autoComplete="off" />
            </Field>
          </form>
        )}
        {error && <Banner tone="danger">{error}</Banner>}
      </div>
    </Dialog>
  );
}
