"use client";

import {
  compactExchangeNumber,
  errorMessage,
  formatExchangePrice,
  useCreateExchangeMarket,
  useExchangeAdminMarkets,
  useExchangeAdminOverview,
  useMe,
  useSetExchangeIndexPrice,
  useSetExchangeMarketStatus,
  useUpdateExchangeMarket,
  type ExchangeAdminMarket,
  type ExchangeCatalogMarket,
  type ExchangeMarketStatus,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, Dialog, EmptyState, ErrorState, Field, Input, Select, TextArea, toast } from "@scaffoldapp/ui-web";
import { CandlestickChart, Plus } from "lucide-react";
import { useState } from "react";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { IndexStatusBadge, Stat, useExchangeMoney } from "./parts";

const STATUS_OPTIONS: { value: ExchangeMarketStatus; label: string }[] = [
  { value: "active", label: "Active" },
  { value: "reduce_only", label: "Reduce only" },
  { value: "halted", label: "Paused" },
];

const EMPTY: ExchangeCatalogMarket = { symbol: "", name: "", category: "", max_leverage: 10, sz_decimals: 2, tick_size: "0.01", min_notional: "10", oi_cap: "1000000" };

function MarketDialog({ market, open, onOpenChange }: { market: ExchangeAdminMarket | null; open: boolean; onOpenChange: (o: boolean) => void }) {
  const create = useCreateExchangeMarket();
  const update = useUpdateExchangeMarket();
  const [form, setForm] = useState<ExchangeCatalogMarket>(market ?? EMPTY);
  const set = <K extends keyof ExchangeCatalogMarket>(k: K, v: ExchangeCatalogMarket[K]) => setForm((f) => ({ ...f, [k]: v }));
  const save = market ? update : create;
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={market ? `Edit ${market.symbol}` : "List a market"}
      info="Changes reach the engine within seconds. Resting orders keep their prices; new orders use the new tick and size steps."
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            loading={save.isPending}
            onClick={() =>
              save.mutate(form, {
                onSuccess: (m) => {
                  toast.success(market ? `${m.symbol} saved` : `${m.symbol} listed`);
                  onOpenChange(false);
                },
                onError: (e) => toast.error(errorMessage(e, "Couldn't save the market.")),
              })
            }
          >
            {market ? "Save" : "List market"}
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Symbol" info="A–Z, 0–9 and '-'. Can't change later.">
          <Input value={form.symbol} disabled={!!market} onChange={(e) => set("symbol", e.target.value.toUpperCase())} />
        </Field>
        <Field label="Name">
          <Input value={form.name} onChange={(e) => set("name", e.target.value)} />
        </Field>
        <Field label="Category">
          <Input value={form.category ?? ""} onChange={(e) => set("category", e.target.value)} />
        </Field>
        <Field label="Price feed key" info="The key the price feed knows this asset by. Empty uses the symbol.">
          <Input value={form.price_feed ?? ""} onChange={(e) => set("price_feed", e.target.value)} />
        </Field>
        <Field label="Max leverage">
          <Input inputMode="numeric" value={String(form.max_leverage)} onChange={(e) => set("max_leverage", Number(e.target.value.replace(/\D/g, "")) || 0)} />
        </Field>
        <Field label="Size decimals">
          <Input inputMode="numeric" value={String(form.sz_decimals)} onChange={(e) => set("sz_decimals", Number(e.target.value.replace(/\D/g, "")) || 0)} />
        </Field>
        <Field label="Tick size">
          <Input inputMode="decimal" value={form.tick_size} onChange={(e) => set("tick_size", e.target.value)} />
        </Field>
        <Field label="Smallest order" info="In the quote currency.">
          <Input inputMode="decimal" value={form.min_notional} onChange={(e) => set("min_notional", e.target.value)} />
        </Field>
        <Field label="Open interest cap" info="In the quote currency.">
          <Input inputMode="decimal" value={form.oi_cap} onChange={(e) => set("oi_cap", e.target.value)} />
        </Field>
        <Field label="Seed price" info="Development only: the price used before the feed reports.">
          <Input inputMode="decimal" value={form.seed_price ?? ""} onChange={(e) => set("seed_price", e.target.value || undefined)} />
        </Field>
        <Field label="Image URL" className="sm:col-span-2">
          <Input value={form.image_url ?? ""} onChange={(e) => set("image_url", e.target.value)} />
        </Field>
        <Field label="Description" className="sm:col-span-2">
          <TextArea value={form.description ?? ""} rows={2} maxLength={2000} onChange={(e) => set("description", e.target.value)} />
        </Field>
      </div>
    </Dialog>
  );
}

function PriceDialog({ market, onOpenChange }: { market: ExchangeAdminMarket | null; onOpenChange: (o: boolean) => void }) {
  const setPrice = useSetExchangeIndexPrice();
  const [price, setPriceText] = useState("");
  const done = (msg: string) => {
    toast.success(msg);
    setPriceText("");
    onOpenChange(false);
  };
  return (
    <Dialog
      open={!!market}
      onOpenChange={onOpenChange}
      title={market ? `Index price for ${market.symbol}` : "Index price"}
      info="A manual price overrides the price feed until you clear it. Liquidations and funding use it."
      footer={
        <>
          {market?.manual_price && (
            <Button
              variant="ghost"
              loading={setPrice.isPending}
              onClick={() => setPrice.mutate({ symbol: market.symbol, price: "" }, { onSuccess: () => done("Back on the price feed"), onError: (e) => toast.error(errorMessage(e)) })}
            >
              Clear manual price
            </Button>
          )}
          <Button
            loading={setPrice.isPending}
            disabled={!(Number(price) > 0)}
            onClick={() => market && setPrice.mutate({ symbol: market.symbol, price }, { onSuccess: () => done("Manual price set"), onError: (e) => toast.error(errorMessage(e)) })}
          >
            Set price
          </Button>
        </>
      }
    >
      <Field label="Price">
        <Input inputMode="decimal" value={price} placeholder={market?.manual_price ?? market?.stats?.index_px ?? ""} onChange={(e) => setPriceText(e.target.value.replace(/[^0-9.]/g, ""))} />
      </Field>
    </Dialog>
  );
}

/** Admin → Exchange: totals, the catalog (list, edit, status), and manual index prices. Changes are audited. */
export function AdminExchangeScreen() {
  const me = useMe();
  const isAdmin = me.data?.admin_role === "admin";
  const { money } = useExchangeMoney();
  const overview = useExchangeAdminOverview();
  const markets = useExchangeAdminMarkets();
  const setStatus = useSetExchangeMarketStatus();
  const [editing, setEditing] = useState<ExchangeAdminMarket | null>(null);
  const [creating, setCreating] = useState(false);
  const [pricing, setPricing] = useState<ExchangeAdminMarket | null>(null);
  const o = overview.data;

  return (
    <div className="flex flex-col gap-section">
      <PageHeader
        title="Exchange"
        action={
          isAdmin ? (
            <Button size="auto" leftIcon={<Plus />} onClick={() => setCreating(true)}>
              List a market
            </Button>
          ) : undefined
        }
      />
      <Section title="Totals" info="The backstop takes over accounts that can't carry their positions; a negative balance is bad debt it covered.">
        {overview.isPending ? (
          <TableSkeleton rows={2} cols={4} />
        ) : overview.isError ? (
          <ErrorState compact title="Couldn't load the totals" info={overview.error.message} onRetry={() => void overview.refetch()} />
        ) : !o?.stats ? (
          <p className="type-label text-warning-strong">The engine is starting or runs on another instance.</p>
        ) : (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            <Stat label="24h volume">
              {compactExchangeNumber(o.stats.day_volume)} {o.config.sim_enabled ? `(${o.stats.day_volume_sim_pct}% simulated)` : ""}
            </Stat>
            <Stat label="Open interest">{money(o.stats.open_interest_value)}</Stat>
            <Stat label="Traders, 24h">{o.stats.traders_24h - o.stats.sim_traders_24h}</Stat>
            <Stat label="Accounts">{o.stats.accounts}</Stat>
            <Stat label="Fees collected">{money(o.stats.fees_collected)}</Stat>
            <Stat label="Backstop balance">{money(o.stats.backstop_balance)}</Stat>
            <Stat label="Pending transfers">{o.pending_transfers}</Stat>
            <Stat label="Simulation">{o.config.sim_enabled ? "On" : "Off"}</Stat>
          </div>
        )}
      </Section>
      <Section title="Markets">
        <Card padding="none">
          {markets.isPending ? (
            <TableSkeleton rows={5} cols={5} />
          ) : markets.isError ? (
            <ErrorState compact title="Couldn't load markets" info={markets.error.message} onRetry={() => void markets.refetch()} />
          ) : markets.data.items.length === 0 ? (
            <EmptyState compact icon={<CandlestickChart />} title="No markets listed" />
          ) : (
            <Table label="Markets">
              <THead>
                <TH>Market</TH>
                <TH className="hidden sm:table-cell" align="right">
                  Mark
                </TH>
                <TH className="hidden md:table-cell">Index</TH>
                <TH className="hidden lg:table-cell" align="right">
                  Max leverage
                </TH>
                <TH>Status</TH>
                <TH align="right">
                  <span className="sr-only">Actions</span>
                </TH>
              </THead>
              <tbody>
                {markets.data.items.map((m) => (
                  <TR key={m.id}>
                    <TD>
                      <span className="block font-semibold text-fg">{m.symbol}</span>
                      <span className="block truncate text-fg-muted">{m.name}</span>
                    </TD>
                    <TD className="hidden tabular-nums sm:table-cell" align="right">
                      {m.stats ? formatExchangePrice(m.stats.mark_px, m.tick_size) : "–"}
                    </TD>
                    <TD className="hidden md:table-cell">
                      <span className="flex items-center gap-2 tabular-nums">
                        {m.stats && m.stats.index_status !== "none" ? formatExchangePrice(m.stats.index_px, m.tick_size) : "–"}
                        {m.stats && <IndexStatusBadge status={m.stats.index_status} />}
                        {m.manual_price && !m.stats && <Badge size="sm">Manual</Badge>}
                      </span>
                    </TD>
                    <TD className="hidden lg:table-cell" align="right">
                      {m.max_leverage}×
                    </TD>
                    <TD>
                      {isAdmin ? (
                        <Select
                          aria-label={`Status of ${m.symbol}`}
                          options={STATUS_OPTIONS}
                          value={m.status ?? "active"}
                          onValueChange={(v) =>
                            setStatus.mutate(
                              { symbol: m.symbol, status: v as ExchangeMarketStatus },
                              { onSuccess: () => toast.success(`${m.symbol} is ${STATUS_OPTIONS.find((s) => s.value === v)?.label.toLowerCase()}`), onError: (e) => toast.error(errorMessage(e)) },
                            )
                          }
                          className="w-36"
                        />
                      ) : (
                        STATUS_OPTIONS.find((s) => s.value === m.status)?.label
                      )}
                    </TD>
                    <TD align="right">
                      {isAdmin && (
                        <span className="flex justify-end gap-2">
                          <Button variant="ghost" size="sm" onClick={() => setPricing(m)}>
                            Price
                          </Button>
                          <Button variant="ghost" size="sm" onClick={() => setEditing(m)}>
                            Edit
                          </Button>
                        </span>
                      )}
                    </TD>
                  </TR>
                ))}
              </tbody>
            </Table>
          )}
        </Card>
      </Section>
      {creating && <MarketDialog market={null} open={creating} onOpenChange={setCreating} />}
      {editing && <MarketDialog key={editing.id} market={editing} open={!!editing} onOpenChange={(open) => !open && setEditing(null)} />}
      <PriceDialog market={pricing} onOpenChange={(open) => !open && setPricing(null)} />
    </div>
  );
}
