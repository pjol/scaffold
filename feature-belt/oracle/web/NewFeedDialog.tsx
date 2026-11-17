"use client";

import { errorMessage, useCreateOracleFeed, useOracleSources, type OracleFeedKind, type OracleProfile } from "@scaffoldapp/shared";
import { Button, Dialog, Field, IconButton, Input, SegmentedControl, Select, TextArea, toast } from "@scaffoldapp/ui-web";
import { Plus, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { oraclePaths } from "./nav";

interface SourceRow {
  source: string;
  venue: string;
  ref: string;
  params: string;
}

interface PartRow {
  key: string;
  quantity: string;
}

const PROFILES: { value: OracleProfile; label: string }[] = [
  { value: "realtime", label: "Realtime: quotes every minute" },
  { value: "intraday", label: "Intraday: trades through the day" },
  { value: "daily", label: "Daily: one price a day plus sales" },
];

const DECIMALS = Array.from({ length: 9 }, (_, i) => ({ value: String(i), label: String(i) }));

/** Admin: create a single (with its venues) or a basket (with its parts). */
export function OracleNewFeedDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const router = useRouter();
  const create = useCreateOracleFeed();
  const sources = useOracleSources({ enabled: open });
  const sourceOptions = (sources.data?.items ?? []).map((s) => ({ value: s.name, label: s.name }));
  const [kind, setKind] = useState<OracleFeedKind>("single");
  const [key, setKey] = useState("");
  const [name, setName] = useState("");
  const [category, setCategory] = useState("");
  const [quote, setQuote] = useState("USD");
  const [decimals, setDecimals] = useState("2");
  const [seed, setSeed] = useState("");
  const [profile, setProfile] = useState<OracleProfile>("intraday");
  const [rows, setRows] = useState<SourceRow[]>([{ source: "manual", venue: "", ref: "", params: "" }]);
  const [parts, setParts] = useState<PartRow[]>([{ key: "", quantity: "1" }]);
  const [error, setError] = useState<string | null>(null);

  function setRow(i: number, patch: Partial<SourceRow>) {
    setRows((rs) => rs.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  }
  function setPart(i: number, patch: Partial<PartRow>) {
    setParts((ps) => ps.map((p, j) => (j === i ? { ...p, ...patch } : p)));
  }

  function submit() {
    setError(null);
    let parsed: { source: string; venue?: string; ref?: string; params?: Record<string, unknown> }[] = [];
    try {
      parsed = rows
        .filter((r) => r.source)
        .map((r) => ({
          source: r.source,
          venue: r.venue.trim() || undefined,
          ref: r.ref.trim() || undefined,
          params: r.params.trim() ? (JSON.parse(r.params) as Record<string, unknown>) : undefined,
        }));
    } catch {
      setError("Params must be a JSON object.");
      return;
    }
    create.mutate(
      {
        key: key.trim().toUpperCase(),
        name: name.trim(),
        category: category.trim() || undefined,
        quote: quote.trim() || undefined,
        decimals: Number(decimals),
        kind,
        seed_price: seed.trim() || undefined,
        profile,
        ...(kind === "single" ? { sources: parsed } : { parts: parts.filter((p) => p.key.trim()).map((p) => ({ key: p.key.trim().toUpperCase(), quantity: p.quantity.trim() })) }),
      },
      {
        onSuccess: (d) => {
          toast.success(`${d.feed.name} created`);
          onOpenChange(false);
          router.push(oraclePaths.feed(d.feed.key));
        },
        onError: (e) => setError(errorMessage(e, "Couldn't create the feed.")),
      },
    );
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title="New feed"
      info="A single is priced from its venues; a basket is the sum of quantity × price over single feeds."
      footer={
        <>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} loading={create.isPending} disabled={!key.trim() || !name.trim()}>
            Create feed
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-6"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <SegmentedControl
          aria-label="Kind"
          options={[
            { value: "single", label: "Single" },
            { value: "basket", label: "Basket" },
          ]}
          value={kind}
          onValueChange={(v) => setKind(v as OracleFeedKind)}
        />
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Key" required info="Upper-case letters, digits, dot, dash or underscore. Agents and links use it.">
            <Input value={key} onChange={(e) => setKey(e.target.value.toUpperCase())} placeholder="GOLD-OZ" autoComplete="off" />
          </Field>
          <Field label="Name" required>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Gold, troy ounce" />
          </Field>
          <Field label="Category">
            <Input value={category} onChange={(e) => setCategory(e.target.value)} placeholder="Metals" />
          </Field>
          <Field label="Quote currency">
            <Input value={quote} onChange={(e) => setQuote(e.target.value.toUpperCase())} />
          </Field>
          <Field label="Decimals">
            <Select options={DECIMALS} value={decimals} onValueChange={setDecimals} />
          </Field>
          <Field label="Seed price" info="A starting estimate until venues set a live price; prints far from it are distrusted at first. Leave empty when unknown.">
            <Input inputMode="decimal" value={seed} onChange={(e) => setSeed(e.target.value)} placeholder="Optional" />
          </Field>
        </div>
        <Field label="Pace" info="Sets the half-life, staleness and how fast the price may move. Change them later in the API.">
          <Select options={PROFILES} value={profile} onValueChange={(v) => setProfile(v as OracleProfile)} />
        </Field>

        {kind === "single" ? (
          <fieldset className="flex flex-col gap-4">
            <legend className="mb-3 type-label font-semibold text-fg">Venues</legend>
            {rows.map((r, i) => (
              <div key={i} className="flex flex-col gap-3 border-b border-border pb-4 last:border-b-0">
                <div className="flex items-end gap-2">
                  <Field label="Source" className="flex-1">
                    <Select options={sourceOptions.length ? sourceOptions : [{ value: r.source, label: r.source }]} value={r.source} onValueChange={(v) => setRow(i, { source: v })} />
                  </Field>
                  <Field label="Venue" className="flex-1" info="Lower-case label compared against the other venues. Defaults to the URL's host or the source name.">
                    <Input value={r.venue} onChange={(e) => setRow(i, { venue: e.target.value.toLowerCase() })} placeholder="Optional" />
                  </Field>
                  <IconButton label="Remove venue" icon={<X />} variant="ghost" onClick={() => setRows((rs) => rs.filter((_, j) => j !== i))} />
                </div>
                {r.source !== "manual" && (
                  <>
                    <Field label="Ref" info="The source's id for this feed: for http, the JSON URL to poll.">
                      <Input value={r.ref} onChange={(e) => setRow(i, { ref: e.target.value })} placeholder="https://" />
                    </Field>
                    <Field label="Params" info='JSON. For http: {"price_path": "data.amount", "every": "60s"}, plus time_path, qty_path, kind and history_* for backfill.'>
                      <TextArea value={r.params} onChange={(e) => setRow(i, { params: e.target.value })} minRows={2} className="font-mono" />
                    </Field>
                  </>
                )}
              </div>
            ))}
            <Button variant="ghost" size="sm" leftIcon={<Plus />} className="self-start" onClick={() => setRows((rs) => [...rs, { source: "http", venue: "", ref: "", params: "" }])}>
              Add venue
            </Button>
          </fieldset>
        ) : (
          <fieldset className="flex flex-col gap-4">
            <legend className="mb-3 type-label font-semibold text-fg">Parts</legend>
            {parts.map((p, i) => (
              <div key={i} className="flex items-end gap-2">
                <Field label="Feed key" className="flex-1">
                  <Input value={p.key} onChange={(e) => setPart(i, { key: e.target.value.toUpperCase() })} placeholder="SILVER-OZ" />
                </Field>
                <Field label="Quantity" className="w-32">
                  <Input inputMode="decimal" value={p.quantity} onChange={(e) => setPart(i, { quantity: e.target.value })} />
                </Field>
                <IconButton label="Remove part" icon={<X />} variant="ghost" onClick={() => setParts((ps) => ps.filter((_, j) => j !== i))} />
              </div>
            ))}
            <Button variant="ghost" size="sm" leftIcon={<Plus />} className="self-start" onClick={() => setParts((ps) => [...ps, { key: "", quantity: "1" }])}>
              Add part
            </Button>
          </fieldset>
        )}
        {error && (
          <p role="alert" className="type-label text-danger-strong">
            {error}
          </p>
        )}
      </form>
    </Dialog>
  );
}
