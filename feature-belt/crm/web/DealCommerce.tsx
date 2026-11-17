"use client";

import {
  CRM_BILLING_PERIOD_LABEL,
  CRM_BILLING_PERIODS,
  crmMoneyInput,
  crmParseMoney,
  crmParsePercent,
  errorMessage,
  useCrmChangeLineItems,
  useCrmCreateQuote,
  useCrmLineItems,
  useCrmProducts,
  useCrmQuotes,
  type CrmBilling,
  type CrmBillingPeriod,
  type CrmDeal,
  type CrmLineItem,
  type CrmLineItemRequest,
  type CrmProduct,
} from "@scaffoldapp/shared";
import {
  Button,
  Card,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  InfoTip,
  Input,
  Menu,
  MenuItem,
  MenuSeparator,
  SegmentedControl,
  Select,
  Skeleton,
  SkeletonGroup,
  Switch,
  TextArea,
  toast,
} from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowUp, FileText, MoreHorizontal, Package, PenLine, Plus, Search, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { useCrm } from "./CrmFrame";
import { QuoteStatusBadge, TotalsList, lineFacts, lineTerm, money, priceText } from "./QuoteDocument";

// ---- the line editor (deals and quote drafts) ---------------------------------------------

/** What the editor shows for a line (a deal line, a quote line or a new one). */
export interface LineDraft {
  product_id?: string | null;
  name: string;
  sku: string;
  description: string;
  unit_price: number;
  quantity: number;
  discount_bp: number;
  discount_amount: number;
  tax_bp: number;
  billing: CrmBilling;
  billing_period: CrmBillingPeriod | null;
  term_periods: number;
}

export function lineFromProduct(p: CrmProduct): LineDraft {
  const term = p.term_periods ?? (p.billing_period ? 12 / { monthly: 1, quarterly: 3, semiannual: 6, annual: 12 }[p.billing_period] : 1);
  return {
    product_id: p.id,
    name: p.name,
    sku: p.sku ?? "",
    description: p.description,
    unit_price: p.unit_price,
    quantity: 1,
    discount_bp: p.discount_bp,
    discount_amount: 0,
    tax_bp: p.tax_bp,
    billing: p.billing,
    billing_period: p.billing_period,
    term_periods: term,
  };
}

export const emptyLine: LineDraft = {
  name: "",
  sku: "",
  description: "",
  unit_price: 0,
  quantity: 1,
  discount_bp: 0,
  discount_amount: 0,
  tax_bp: 0,
  billing: "one_time",
  billing_period: null,
  term_periods: 1,
};

/** A line as the API takes it (a product line keeps its product reference). */
export function lineRequest(l: LineDraft, withProduct: boolean): CrmLineItemRequest {
  return {
    ...(withProduct && l.product_id ? { product_id: l.product_id } : {}),
    name: l.name,
    sku: l.sku,
    description: l.description,
    unit_price: l.unit_price,
    quantity: l.quantity,
    discount_bp: l.discount_bp,
    discount_amount: l.discount_amount,
    tax_bp: l.tax_bp,
    billing: l.billing,
    billing_period: l.billing_period ?? "",
    term_periods: l.term_periods,
  };
}

/** Edit one line: quantity, price, discount (percent or amount), tax and billing. */
export function LineEditorDialog({
  title,
  currency,
  initial,
  saving,
  onSave,
  onClose,
}: {
  title: string;
  currency: string;
  initial: LineDraft;
  saving: boolean;
  onSave: (l: LineDraft) => void;
  onClose: () => void;
}) {
  const [name, setName] = useState(initial.name);
  const [description, setDescription] = useState(initial.description);
  const [qty, setQty] = useState(String(initial.quantity));
  const [price, setPrice] = useState(crmMoneyInput(initial.unit_price, currency));
  const [discountKind, setDiscountKind] = useState<"percent" | "amount">(initial.discount_amount > 0 ? "amount" : "percent");
  const [discount, setDiscount] = useState(initial.discount_amount > 0 ? crmMoneyInput(initial.discount_amount, currency) : initial.discount_bp ? String(initial.discount_bp / 100) : "");
  const [tax, setTax] = useState(initial.tax_bp ? String(initial.tax_bp / 100) : "");
  const [billing, setBilling] = useState<CrmBilling>(initial.billing);
  const [period, setPeriod] = useState<CrmBillingPeriod>(initial.billing_period ?? "monthly");
  const [term, setTerm] = useState(String(initial.term_periods || 1));
  const [error, setError] = useState<string | null>(null);
  const save = () => {
    if (!name.trim()) return setError("Give the line a name.");
    const q = Number(qty.replace(",", "."));
    if (!Number.isFinite(q) || q <= 0 || Math.round(q * 1000) / 1000 !== q || q > 1_000_000) return setError("Quantity is a number above 0 with up to 3 decimals.");
    const unit = crmParseMoney(price, currency);
    if (unit === null) return setError("Enter the unit price as a number, like 1200.");
    let bp = 0;
    let amount = 0;
    if (discount.trim()) {
      if (discountKind === "percent") {
        const v = crmParsePercent(discount);
        if (v === null) return setError("A discount is 0 to 100 %.");
        bp = v;
      } else {
        const v = crmParseMoney(discount, currency);
        if (v === null) return setError("Enter the discount as an amount, like 50.");
        amount = v;
      }
    }
    const taxBp = crmParsePercent(tax);
    if (taxBp === null) return setError("Tax is 0 to 100 %.");
    const t = Number(term);
    if (billing === "recurring" && (!Number.isInteger(t) || t < 1 || t > 120)) return setError("A term is 1 to 120 billing periods.");
    setError(null);
    onSave({
      ...initial,
      name: name.trim(),
      description: description.trim(),
      quantity: q,
      unit_price: unit,
      discount_bp: bp,
      discount_amount: amount,
      tax_bp: taxBp,
      billing,
      billing_period: billing === "recurring" ? period : null,
      term_periods: billing === "recurring" ? t : 1,
    });
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={title}
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button loading={saving} onClick={save}>
            Save line
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        <Field label="Name" required error={error}>
          <Input value={name} maxLength={200} autoFocus onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Description">
          <TextArea value={description} maxLength={2000} rows={2} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Quantity">
            <Input value={qty} inputMode="decimal" onChange={(e) => setQty(e.target.value)} />
          </Field>
          <Field label="Unit price">
            <Input value={price} inputMode="decimal" onChange={(e) => setPrice(e.target.value)} trailing={<span className="px-2 type-caption">{currency}</span>} />
          </Field>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Discount">
            <div className="flex gap-2">
              <SegmentedControl
                aria-label="Discount kind"
                fullWidth={false}
                value={discountKind}
                onValueChange={(v) => setDiscountKind(v)}
                options={[
                  { value: "percent", label: "%" },
                  { value: "amount", label: currency },
                ]}
              />
              <Input value={discount} inputMode="decimal" onChange={(e) => setDiscount(e.target.value)} placeholder="0" wrapperClassName="flex-1" />
            </div>
          </Field>
          <Field label="Tax" info="A percentage of the line after its discount.">
            <Input value={tax} inputMode="decimal" onChange={(e) => setTax(e.target.value)} placeholder="0" trailing={<span className="px-2 type-caption">%</span>} />
          </Field>
        </div>
        <Field label="Billing">
          <SegmentedControl
            value={billing}
            onValueChange={(v) => setBilling(v)}
            options={[
              { value: "one_time", label: "One time" },
              { value: "recurring", label: "Recurring" },
            ]}
          />
        </Field>
        {billing === "recurring" ? (
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Bills">
              <Select value={period} onValueChange={(v) => setPeriod(v as CrmBillingPeriod)} options={CRM_BILLING_PERIODS.map((p) => ({ value: p, label: CRM_BILLING_PERIOD_LABEL[p].every }))} />
            </Field>
            <Field label="Term" info="How many billing periods the contract runs; the contract value counts the line that many times.">
              <Input value={term} inputMode="numeric" onChange={(e) => setTerm(e.target.value)} trailing={<span className="px-2 type-caption">periods</span>} />
            </Field>
          </div>
        ) : null}
        <button type="submit" hidden aria-hidden tabIndex={-1} />
      </form>
    </Dialog>
  );
}

/** Pick a product from the library (active ones, by name or SKU). */
export function ProductPickerDialog({ orgId, currency, onPick, onClose }: { orgId: string; currency: string; onPick: (p: CrmProduct) => void; onClose: () => void }) {
  const [q, setQ] = useState("");
  const [debounced, setDebounced] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 200);
    return () => clearTimeout(t);
  }, [q]);
  const products = useCrmProducts(orgId, debounced);
  const items = products.data?.items ?? [];
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title="Add a product" size="md">
      <div className="flex flex-col gap-3">
        <Input aria-label="Search products" placeholder="Search name or SKU" value={q} autoFocus onChange={(e) => setQ(e.target.value)} leadingIcon={<Search />} />
        {products.isPending ? (
          <SkeletonGroup label="Loading products" className="flex flex-col gap-2">
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
          </SkeletonGroup>
        ) : products.isError ? (
          <ErrorState compact title="Couldn't load products" info={products.error.message} onRetry={() => void products.refetch()} />
        ) : items.length === 0 ? (
          <EmptyState
            compact
            icon={<Package />}
            title={debounced ? "No products match" : "No products yet"}
            action={
              <Button asChild size="auto" variant="secondary">
                <Link href="/crm/products">Products</Link>
              </Button>
            }
          />
        ) : (
          <ul className="flex max-h-96 flex-col gap-1 overflow-y-auto">
            {items.map((p) => (
              <li key={p.id}>
                <button
                  type="button"
                  disabled={p.currency !== currency}
                  onClick={() => onPick(p)}
                  className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <Package className="size-4 shrink-0 text-fg-muted" aria-hidden />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate type-label font-semibold text-fg">{p.name}</span>
                    <span className="truncate type-caption text-fg-muted">
                      {[p.sku, p.currency !== currency ? `Priced in ${p.currency}` : null].filter(Boolean).join(" · ")}
                    </span>
                  </span>
                  <span className="shrink-0 type-label tabular-nums text-fg">{priceText(p.unit_price, p.currency, p.billing, p.billing_period)}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Dialog>
  );
}

/** One line in a list: name, facts, schedule, total; actions in its ⋮. */
export function LineRow({
  line,
  currency,
  actions,
}: {
  line: Pick<CrmLineItem, "name" | "sku" | "description" | "quantity" | "unit_price" | "discount_bp" | "discount_amount" | "tax_bp" | "billing" | "billing_period" | "term_periods" | "total" | "contract_value">;
  currency: string;
  actions?: React.ReactNode;
}) {
  const term = lineTerm(line, currency);
  return (
    // Narrow columns (the record page's side rail) put the amount under the facts.
    <li className="@container flex items-start gap-2 py-2">
      <div className="flex min-w-0 flex-1 flex-col gap-0.5 @xs:flex-row @xs:items-start @xs:gap-2">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="type-label font-semibold text-fg [overflow-wrap:anywhere]">{line.name}</span>
          <span className="type-caption text-fg-muted">{lineFacts(line, currency).join(" · ")}</span>
          {term ? <span className="type-caption text-fg-muted">{term}</span> : null}
        </div>
        <span className="type-label font-semibold tabular-nums text-fg @xs:shrink-0 @xs:text-right">
          {money(line.total, currency)}
          {line.billing === "recurring" && line.billing_period ? <span className="font-normal text-fg-muted"> {CRM_BILLING_PERIOD_LABEL[line.billing_period].per}</span> : null}
        </span>
      </div>
      {actions}
    </li>
  );
}

// ---- the deal's line items ------------------------------------------------------------------

/** The deal's line items: products, quantities, prices, discounts, tax, totals; the amount can follow them. */
export function CrmDealLineItems({ deal }: { deal: CrmDeal }) {
  const { orgId } = useCrm();
  const items = useCrmLineItems(deal.id);
  const change = useCrmChangeLineItems(deal.id);
  const [picking, setPicking] = useState(false);
  const [editing, setEditing] = useState<{ id?: string; line: LineDraft; title: string } | null>(null);
  const fail = (fallback: string) => (e: unknown) => toast.error(errorMessage(e, fallback));
  const data = items.data;
  const currency = data?.currency ?? deal.currency;

  const save = (l: LineDraft) => {
    if (!editing) return;
    const body = lineRequest(l, !editing.id);
    change.mutate(editing.id ? { op: "update", id: editing.id, body: { ...body, product_id: undefined } } : { op: "add", body }, {
      onSuccess: () => {
        toast.success(editing.id ? "Line saved" : "Line added");
        setEditing(null);
      },
      onError: fail("Couldn't save the line."),
    });
  };
  const move = (from: number, to: number) => {
    if (!data) return;
    const ids = data.items.map((i) => i.id);
    const [x] = ids.splice(from, 1);
    if (!x) return;
    ids.splice(to, 0, x);
    change.mutate({ op: "reorder", ids }, { onError: fail("Couldn't move the line.") });
  };

  return (
    <Card padding="compact" className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <h2 className="flex-1 type-title-sm text-fg">
          Line items <span className="type-label text-fg-muted">{data?.items.length || ""}</span>
        </h2>
        {data?.can_edit ? (
          <Menu trigger={<Button variant="ghost" size="sm" leftIcon={<Plus />}>Add</Button>}>
            <MenuItem icon={<Package />} onSelect={() => setPicking(true)}>
              From products…
            </MenuItem>
            <MenuItem icon={<PenLine />} onSelect={() => setEditing({ line: emptyLine, title: "New line" })}>
              Custom line
            </MenuItem>
          </Menu>
        ) : null}
      </div>
      {items.isPending ? (
        <SkeletonGroup label="Loading line items" className="flex flex-col gap-2">
          <Skeleton shape="text" className="h-4 w-full" />
          <Skeleton shape="text" className="h-4 w-2/3" />
        </SkeletonGroup>
      ) : items.isError ? (
        <ErrorState compact title="Couldn't load line items" info={items.error.message} onRetry={() => void items.refetch()} />
      ) : data && data.items.length === 0 ? (
        <p className="type-label text-fg-subtle">No line items</p>
      ) : data ? (
        <>
          <ul className="flex flex-col divide-y divide-border">
            {data.items.map((li, i) => (
              <LineRow
                key={li.id}
                line={li}
                currency={currency}
                actions={
                  data.can_edit ? (
                    <Menu trigger={<IconButton label={`Actions for ${li.name}`} size="sm" icon={<MoreHorizontal />} />}>
                      <MenuItem icon={<PenLine />} onSelect={() => setEditing({ id: li.id, line: { ...li }, title: "Edit line" })}>
                        Edit
                      </MenuItem>
                      {i > 0 ? (
                        <MenuItem icon={<ArrowUp />} onSelect={() => move(i, i - 1)}>
                          Move up
                        </MenuItem>
                      ) : null}
                      {i < data.items.length - 1 ? (
                        <MenuItem icon={<ArrowDown />} onSelect={() => move(i, i + 1)}>
                          Move down
                        </MenuItem>
                      ) : null}
                      <MenuSeparator />
                      <MenuItem icon={<Trash2 />} destructive onSelect={() => change.mutate({ op: "remove", id: li.id }, { onSuccess: () => toast.success("Line removed"), onError: fail("Couldn't remove the line.") })}>
                        Remove
                      </MenuItem>
                    </Menu>
                  ) : null
                }
              />
            ))}
          </ul>
          <TotalsList totals={data.totals} currency={currency} />
        </>
      ) : null}
      {data ? (
        <Switch
          checked={data.amount_from_items}
          disabled={!data.can_edit || change.isPending}
          onCheckedChange={(v) => change.mutate({ op: "amount_source", fromItems: v }, { onSuccess: () => toast.success(v ? "The amount follows the line items" : "You set the amount yourself now"), onError: fail("Couldn't change it.") })}
          label="Amount follows line items"
          info="While on, the deal's amount is the line items' contract value and changes with them; typing an amount needs it off."
        />
      ) : null}
      {picking ? (
        <ProductPickerDialog
          orgId={orgId}
          currency={currency}
          onClose={() => setPicking(false)}
          onPick={(p) => {
            setPicking(false);
            setEditing({ line: lineFromProduct(p), title: `Add ${p.name}` });
          }}
        />
      ) : null}
      {editing ? <LineEditorDialog title={editing.title} currency={currency} initial={editing.line} saving={change.isPending} onSave={save} onClose={() => setEditing(null)} /> : null}
    </Card>
  );
}

// ---- the deal's quotes ------------------------------------------------------------------------

/** The deal's quotes: status, total, views; New quote copies the line items into a draft. */
export function CrmDealQuotes({ deal }: { deal: CrmDeal }) {
  const quotes = useCrmQuotes(deal.id);
  const create = useCrmCreateQuote(deal.id);
  const router = useRouter();
  const canEdit = deal.my_level === "edit" || deal.my_level === "manage";
  return (
    <Card padding="compact" className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <h2 className="flex-1 type-title-sm text-fg">
          Quotes <span className="type-label text-fg-muted">{quotes.data?.items.length || ""}</span>
        </h2>
        <InfoTip label="About quotes" content="A quote copies the deal's line items into a draft you can edit. Publishing it makes a link the buyer opens to accept or decline." />
        {canEdit ? (
          <Button
            variant="ghost"
            size="sm"
            leftIcon={<Plus />}
            loading={create.isPending}
            onClick={() => create.mutate(undefined, { onSuccess: (q) => router.push(`/crm/quotes/${q.id}`), onError: (e) => toast.error(errorMessage(e, "Couldn't make the quote.")) })}
          >
            New
          </Button>
        ) : null}
      </div>
      {quotes.isPending ? (
        <SkeletonGroup label="Loading quotes" className="flex flex-col gap-2">
          <Skeleton shape="text" className="h-4 w-full" />
        </SkeletonGroup>
      ) : quotes.isError ? (
        <ErrorState compact title="Couldn't load quotes" info={quotes.error.message} onRetry={() => void quotes.refetch()} />
      ) : quotes.data.items.length === 0 ? (
        <p className="type-label text-fg-subtle">No quotes</p>
      ) : (
        <ul className="flex flex-col gap-1">
          {quotes.data.items.map((q) => (
            <li key={q.id} className="-mx-2 rounded-md px-2 py-1.5 hover:bg-sunken">
              <Link href={`/crm/quotes/${q.id}`} className="flex items-start gap-2">
                <FileText className="mt-0.5 size-4 shrink-0 text-fg-muted" aria-hidden />
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate type-label font-semibold text-fg">
                    {q.number} · {q.title}
                  </span>
                  <span className="flex flex-wrap items-center gap-2 type-caption text-fg-muted">
                    <QuoteStatusBadge status={q.status} expired={q.expired} />
                    <span className="tabular-nums">{money(q.total, q.currency)}</span>
                    {q.view_count > 0 ? <span>{q.view_count === 1 ? "1 view" : `${q.view_count} views`}</span> : null}
                  </span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

