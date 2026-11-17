"use client";

import {
  basketsReplaceLine,
  errorMessage,
  formatBasketsWeight,
  useBasket,
  useBasketsPreview,
  useBasketsSearch,
  useCreateBasket,
  useDebouncedValue,
  type BasketsCandidate,
  type BasketsPreview,
  type BasketsPreviewLine,
} from "@scaffoldapp/shared";
import { Banner, Button, EmptyState, ErrorState, Field, IconButton, InfoTip, Input, Skeleton, SkeletonGroup, Stepper, TextArea, toast } from "@scaffoldapp/ui-web";
import { CircleAlert, CircleCheck, CircleX, ListPlus, Search, X } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { Stat, useExchangeMoney } from "../exchange/parts";
import { BasketLines } from "./parts";
import { basketsPaths } from "./registry";

interface PickRow {
  symbol: string;
  name: string;
  quantity: number;
}

const LIST_INFO = "One market per line: “2 Gold”, “Silver x3” or “BTC-USD, 1”. A CSV with a header row works too. The quantity defaults to 1.";

function LineIcon({ status }: { status: BasketsPreviewLine["status"] }) {
  if (status === "resolved") return <CircleCheck className="size-4 shrink-0 text-success-strong" aria-label="Matched" />;
  if (status === "unpriced") return <CircleAlert className="size-4 shrink-0 text-warning-strong" aria-label="No price" />;
  return <CircleX className="size-4 shrink-0 text-danger-strong" aria-label="No match" />;
}

/** Each input line as the server read it, with fixes for the ones that didn't match. */
function PreviewLines({
  lines,
  money,
  onUse,
  onRemove,
}: {
  lines: BasketsPreviewLine[];
  money: (v: string | null) => string;
  onUse: (line: BasketsPreviewLine, c: BasketsCandidate) => void;
  onRemove: (line: BasketsPreviewLine) => void;
}) {
  return (
    <ul aria-label="Lines" className="flex flex-col">
      {lines.map((l, i) => (
        <li key={`${l.line}-${l.text}-${i}`} className="flex flex-col gap-2 border-b border-border py-2 last:border-b-0">
          <div className="flex min-h-control items-center gap-3">
            <LineIcon status={l.status} />
            <span className="flex min-w-0 flex-1 flex-col">
              <span className="truncate type-label text-fg">
                {l.quantity} × {l.market ? l.market.name : `“${l.text}”`}
              </span>
              <span className="truncate type-caption text-fg-muted">
                {l.market ? `${l.market.symbol}${l.match === "fuzzy" ? ` · read from “${l.text}”` : ""}` : l.message}
                {l.market && l.message ? ` · ${l.message}` : ""}
              </span>
            </span>
            {l.price && <span className="type-label tabular-nums text-fg">{money(l.price)}</span>}
            {l.status === "unresolved" && <IconButton label={`Remove “${l.text}”`} icon={<X />} variant="ghost" size="sm" onClick={() => onRemove(l)} />}
          </div>
          {l.status === "unresolved" && l.candidates.length > 0 && (
            <div className="flex flex-wrap gap-2 pl-7">
              {l.candidates.map((c) => (
                <Button key={c.symbol} size="sm" variant="secondary" onClick={() => onUse(l, c)}>
                  {c.name} ({c.symbol})
                </Button>
              ))}
            </div>
          )}
        </li>
      ))}
    </ul>
  );
}

function PreviewPanel({
  p,
  money,
  onUse,
  onRemove,
}: {
  p: BasketsPreview;
  money: (v: string | null) => string;
  onUse: (line: BasketsPreviewLine, c: BasketsCandidate) => void;
  onRemove: (line: BasketsPreviewLine) => void;
}) {
  const ex = p.existing;
  return (
    <div className="flex flex-col gap-6">
      {ex && !ex.hidden && (
        <Banner tone="info" action={<Link href={basketsPaths.basket(ex.symbol)} className="type-label underline underline-offset-2">Open it</Link>}>
          {ex.status === "archived"
            ? `These lines make ${ex.name} (${ex.symbol}), now archived. Creating it reopens it.`
            : ex.entered
              ? `These lines make ${ex.name} (${ex.symbol}). You're in it already.`
              : `These lines make ${ex.name} (${ex.symbol}), listed already. Creating it adds you to its ${ex.entries} ${ex.entries === 1 ? "entry" : "entries"}.`}
        </Banner>
      )}
      {p.blockers.length > 0 && (
        <Banner tone="danger">
          <ul className="flex flex-col gap-1">
            {p.blockers.map((b) => (
              <li key={b}>{b}</li>
            ))}
          </ul>
        </Banner>
      )}
      {p.warnings.length > 0 && (
        <Banner tone="warning">
          <ul className="flex flex-col gap-1">
            {p.warnings.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        </Banner>
      )}
      <PreviewLines lines={p.lines} money={money} onUse={onUse} onRemove={onRemove} />
      {p.basket.length > 0 && (
        <div className="flex flex-col gap-3">
          <h3 className="type-label font-semibold text-fg">One basket unit</h3>
          <BasketLines lines={p.basket} money={money} label="One basket unit" />
        </div>
      )}
      {p.basket.length > 0 && (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
          <Stat label="Value">{money(p.value)}</Stat>
          <Stat label="Largest line">{formatBasketsWeight(p.max_share)}</Stat>
          <Stat label="Symbol">{p.symbol ?? "–"}</Stat>
          {p.spec && (
            <>
              <Stat label="Max leverage">{p.spec.max_leverage}×</Stat>
              <Stat label="Tick size">{p.spec.tick_size}</Stat>
              <Stat label="Minimum order">{money(p.spec.min_notional)}</Stat>
              <Stat label="Open interest cap">{money(p.spec.oi_cap)}</Stat>
            </>
          )}
        </div>
      )}
    </div>
  );
}

/** /baskets/new: paste a list or pick markets, see the basket priced live, and list it (or join the same one). */
export function NewBasketScreen() {
  const router = useRouter();
  const params = useSearchParams();
  const from = params.get("from");
  const { money } = useExchangeMoney();
  const [text, setText] = useState("");
  const [picks, setPicks] = useState<PickRow[]>([]);
  const [query, setQuery] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [endsOn, setEndsOn] = useState("");
  const create = useCreateBasket();

  // ?from=SYMBOL starts from another basket's lines.
  const source = useBasket(from);
  const prefilled = useRef(false);
  useEffect(() => {
    if (prefilled.current || !source.data) return;
    prefilled.current = true;
    setText(source.data.lines.map((l) => `${l.quantity} ${l.symbol}`).join("\n"));
  }, [source.data]);

  const input = useMemo(() => ({ text, picks: picks.map(({ symbol, quantity }) => ({ symbol, quantity })) }), [text, picks]);
  const debounced = useDebouncedValue(input, 400);
  const preview = useBasketsPreview(debounced);
  const searchQ = useDebouncedValue(query.trim(), 250);
  const search = useBasketsSearch(searchQ, searchQ.length > 0);
  const empty = !text.trim() && picks.length === 0;
  const fresh = debounced === input && !preview.isPlaceholderData && !preview.isFetching;
  const p = empty ? undefined : preview.data;
  const ex = p?.existing ?? null;

  const addPick = (c: Pick<BasketsCandidate, "symbol" | "name">, quantity = 1) =>
    setPicks((ps) => {
      const i = ps.findIndex((x) => x.symbol === c.symbol);
      if (i < 0) return [...ps, { symbol: c.symbol, name: c.name, quantity }];
      return ps.map((x, j) => (j === i ? { ...x, quantity: Math.min(10_000, x.quantity + quantity) } : x));
    });
  const removeLine = (l: BasketsPreviewLine) => {
    if (l.line > 0) setText((t) => basketsReplaceLine(t, l.line, null));
    else setPicks((ps) => ps.filter((x) => x.symbol !== l.text));
  };
  const applyCandidate = (l: BasketsPreviewLine, c: BasketsCandidate) => {
    removeLine(l);
    addPick(c, l.quantity);
  };

  const submit = async () => {
    try {
      const res = await create.mutateAsync({ ...input, name: name.trim() || undefined, description: description.trim() || undefined, ends_on: endsOn || null });
      toast.success(res.created ? "Basket listed" : res.revived ? "Basket reopened" : res.joined ? "You're in this basket" : "You were in this basket already");
      router.push(basketsPaths.basket(res.basket.symbol));
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't create the basket. Try again."));
    }
  };

  const label = !ex ? "Create basket" : ex.status === "archived" ? `Reopen ${ex.symbol}` : `Join ${ex.symbol}`;
  const disabled = !p || !p.creatable || !fresh || create.isPending || (!!ex && ex.entered && ex.status !== "archived");

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
      <PageHeader title="New basket" info="One basket unit holds each line's quantity of its market. It's priced from those markets, and the same lines always make the same basket, whoever lists it." />
      <div className="grid gap-section lg:grid-cols-2 lg:items-start">
        <div className="flex flex-col gap-section">
          <Section title="Markets" info={LIST_INFO}>
            <div className="flex flex-col gap-4">
              <Field label="Paste a list">
                <TextArea value={text} onChange={(e) => setText(e.target.value)} minRows={6} maxRows={16} placeholder={"1 Gold\nSilver x40"} spellCheck={false} />
              </Field>
              <Field label="Add from search">
                <Input type="search" leadingIcon={<Search />} value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Market name or symbol" />
              </Field>
              {searchQ.length > 0 &&
                (search.isPending ? (
                  <SkeletonGroup label="Searching" className="flex flex-col gap-2">
                    <Skeleton className="h-10" />
                    <Skeleton className="h-10" />
                  </SkeletonGroup>
                ) : search.isError ? (
                  <ErrorState compact title="Couldn't search markets" info={search.error.message} onRetry={() => void search.refetch()} />
                ) : search.data.items.length === 0 ? (
                  <p className="type-label text-fg-muted">No markets match “{searchQ}”.</p>
                ) : (
                  <ul aria-label="Search results" className="flex flex-col">
                    {search.data.items.slice(0, 6).map((c) => (
                      <li key={c.symbol} className="flex min-h-row items-center gap-3 border-b border-border last:border-b-0">
                        <span className="flex min-w-0 flex-1 flex-col">
                          <span className="truncate type-label text-fg">{c.name}</span>
                          <span className="truncate type-caption text-fg-muted">
                            {c.symbol}
                            {c.category ? ` · ${c.category}` : ""}
                          </span>
                        </span>
                        <Button size="sm" variant="secondary" leftIcon={<ListPlus />} onClick={() => addPick(c)}>
                          Add
                        </Button>
                      </li>
                    ))}
                  </ul>
                ))}
              {picks.length > 0 && (
                <ul aria-label="Picked markets" className="flex flex-col">
                  {picks.map((pk) => (
                    <li key={pk.symbol} className="flex min-h-row items-center gap-3 border-b border-border py-1 last:border-b-0">
                      <span className="flex min-w-0 flex-1 flex-col">
                        <span className="truncate type-label text-fg">{pk.name}</span>
                        <span className="type-caption text-fg-muted">{pk.symbol}</span>
                      </span>
                      <Stepper
                        value={pk.quantity}
                        min={1}
                        max={10_000}
                        label={`Quantity of ${pk.name}`}
                        onValueChange={(n) => setPicks((ps) => ps.map((x) => (x.symbol === pk.symbol ? { ...x, quantity: n } : x)))}
                      />
                      <IconButton label={`Remove ${pk.name}`} icon={<X />} variant="ghost" size="sm" onClick={() => setPicks((ps) => ps.filter((x) => x.symbol !== pk.symbol))} />
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </Section>
          {!ex && (
            <Section title="Details">
              <div className="flex max-w-form flex-col gap-4">
                <Field label="Name" info="3–48 characters. Left empty, it's named after its largest lines.">
                  <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={48} placeholder="Named after its largest lines" />
                </Field>
                <Field label="Description">
                  <TextArea value={description} onChange={(e) => setDescription(e.target.value)} maxLength={500} minRows={2} maxRows={6} />
                </Field>
                <Field label="End date" info="Optional. Some time after this date the basket winds down: only closing orders while people hold positions, then archived.">
                  <Input type="date" value={endsOn} onChange={(e) => setEndsOn(e.target.value)} />
                </Field>
              </div>
            </Section>
          )}
        </div>

        <div className="flex flex-col gap-6 lg:sticky lg:top-20">
          <Section title="Preview">
            {empty ? (
              <EmptyState compact icon={<ListPlus />} title="Add markets to see the basket" />
            ) : !p && preview.isError ? (
              <ErrorState compact title="Couldn't check this list" info={preview.error.message} onRetry={() => void preview.refetch()} retrying={preview.isFetching} />
            ) : !p ? (
              <SkeletonGroup label="Checking the list" className="flex flex-col gap-3">
                <Skeleton className="h-10" />
                <Skeleton className="h-10" />
                <Skeleton className="h-24" />
              </SkeletonGroup>
            ) : (
              <PreviewPanel p={p} money={money} onUse={applyCandidate} onRemove={removeLine} />
            )}
          </Section>
          <div className="flex items-center gap-3">
            {ex?.entered && ex.status !== "archived" ? (
              <Button asChild variant="secondary">
                <Link href={basketsPaths.basket(ex.symbol)}>Open {ex.symbol}</Link>
              </Button>
            ) : (
              <Button onClick={() => void submit()} disabled={disabled} loading={create.isPending}>
                {label}
              </Button>
            )}
            <InfoTip content="Your display name appears on baskets you list. Listing a basket creates a market; it doesn't place a trade." />
          </div>
        </div>
      </div>
    </div>
  );
}
