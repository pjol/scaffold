"use client";

import { errorMessage, useExchangeFaucet, useNow, type ExchangeAccount } from "@scaffoldapp/shared";
import { Button, cn, toast } from "@scaffoldapp/ui-web";
import { Droplets } from "lucide-react";
import { signTone, Stat, useExchangeMoney } from "./parts";

/** The faucet button when the exchange has one (test funds, once per cooldown). */
export function FaucetButton({ account, className }: { account: ExchangeAccount; className?: string }) {
  const { money, config } = useExchangeMoney();
  const faucet = useExchangeFaucet();
  const now = useNow(30_000, !!account.faucet_available_at);
  if (!config?.faucet_amount) return null;
  const next = account.faucet_available_at ? Date.parse(account.faucet_available_at) : 0;
  const waiting = next > now;
  if (Number(account.balance) >= Number(config.faucet_amount)) return null; // nothing to top up
  return (
    <Button
      variant="secondary"
      size="sm"
      leftIcon={<Droplets />}
      className={className}
      disabled={waiting}
      loading={faucet.isPending}
      title={waiting ? `Available again at ${new Date(next).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}` : undefined}
      onClick={() =>
        faucet.mutate(undefined, {
          onSuccess: () => toast.success(`Balance topped up to ${money(config.faucet_amount)}`),
          onError: (e) => toast.error(errorMessage(e, "Couldn't claim test funds.")),
        })
      }
    >
      {waiting ? "Test funds used" : `Top up to ${money(config.faucet_amount)}`}
    </Button>
  );
}

/** Account value, PnL and margin at a glance. */
export function AccountSummary({ account, compact, className }: { account: ExchangeAccount; compact?: boolean; className?: string }) {
  const { money } = useExchangeMoney();
  return (
    <div className={cn("grid gap-4", compact ? "grid-cols-2" : "grid-cols-2 sm:grid-cols-3 lg:grid-cols-6", className)}>
      <Stat label="Account value">{money(account.account_value)}</Stat>
      <Stat label="Profit and loss">
        <span className={signTone(account.pnl)}>{money(account.pnl, true)}</span>
      </Stat>
      <Stat label="Free collateral">{money(account.free_collateral)}</Stat>
      <Stat label="Margin used">{money(account.margin_used)}</Stat>
      {!compact && (
        <>
          <Stat label="Unrealized PnL">
            <span className={signTone(account.unrealized_pnl)}>{money(account.unrealized_pnl, true)}</span>
          </Stat>
          <Stat label="Balance">{money(account.balance)}</Stat>
        </>
      )}
    </div>
  );
}
