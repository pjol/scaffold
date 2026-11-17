"use client";

import {
  CRM_BILLING_PERIOD_LABEL,
  CRM_QUOTE_STATUS_LABEL,
  crmMoney,
  crmPercent,
  type CrmBilling,
  type CrmBillingPeriod,
  type CrmQuoteItem,
  type CrmQuoteStatus,
  type CrmTotals,
} from "@scaffoldapp/shared";
import { Badge, InfoTip, cn, type BadgeTone } from "@scaffoldapp/ui-web";
import { useEffect, type ReactNode, type RefObject } from "react";
import { formatDate } from "@/lib/format";

// Pieces every commerce surface shares (deal card, quote screen, the buyer's public page):
// money, quantities, billing text, the totals list and the printable quote document. Nothing
// here reads the CRM frame, so the public page can use it without the signed-in app.

// ---- status ---------------------------------------------------------------------------

export type QuoteStatusKey = CrmQuoteStatus | "expired";

export const QUOTE_TONE: Record<QuoteStatusKey, BadgeTone> = { draft: "neutral", published: "info", accepted: "success", declined: "danger", expired: "warning" };

/** "expired" for a published quote past its expiry, else the stored status. */
export function quoteStatusKey(q: { status: CrmQuoteStatus; expired?: boolean }): QuoteStatusKey {
  return q.expired && q.status === "published" ? "expired" : q.status;
}

export function QuoteStatusBadge({ status, expired, size }: { status: CrmQuoteStatus; expired?: boolean; size?: "sm" | "md" }) {
  const k = quoteStatusKey({ status, ...(expired !== undefined ? { expired } : {}) });
  return (
    <Badge tone={QUOTE_TONE[k]} dot={k !== "draft"} {...(size ? { size } : {})}>
      {CRM_QUOTE_STATUS_LABEL[k]}
    </Badge>
  );
}

// ---- money, quantities, billing ---------------------------------------------------------

/** Commerce amounts always show minor units ("$1,200.00") so columns line up. */
export function money(minor: number, currency: string): string {
  return crmMoney(minor, currency, { cents: true });
}

const qtyFormat = new Intl.NumberFormat(undefined, { maximumFractionDigits: 3 });

/** 1.5 → "1.5", 1000 → "1,000". */
export function quantityText(q: number): string {
  return qtyFormat.format(q);
}

const MONTHS: Record<CrmBillingPeriod, number> = { monthly: 1, quarterly: 3, semiannual: 6, annual: 12 };
const TERM_UNIT: Record<CrmBillingPeriod, [string, string]> = {
  monthly: ["month", "months"],
  quarterly: ["quarter", "quarters"],
  semiannual: ["half-year", "half-years"],
  annual: ["year", "years"],
};

/** The unit a term is typed in: "months", "quarters"… */
export function termUnit(period: CrmBillingPeriod, n: number): string {
  return n === 1 ? TERM_UNIT[period][0] : TERM_UNIT[period][1];
}

/** A term as a length of time: 12 monthly periods → "1 year", 6 → "6 months". */
export function termDuration(period: CrmBillingPeriod, periods: number): string {
  const months = periods * MONTHS[period];
  if (months % 12 === 0) return `${months / 12} ${months === 12 ? "year" : "years"}`;
  return `${months} ${months === 1 ? "month" : "months"}`;
}

/** A price with its schedule: "$49.00 / month" or "$1,200.00". */
export function priceText(minor: number, currency: string, billing: CrmBilling, period: CrmBillingPeriod | null): string {
  return billing === "recurring" && period ? `${money(minor, currency)} ${CRM_BILLING_PERIOD_LABEL[period].per}` : money(minor, currency);
}

/** What a line needs for its display (deal line items and quote lines both fit). */
export type LineLike = Pick<
  CrmQuoteItem,
  "name" | "sku" | "description" | "quantity" | "unit_price" | "discount_bp" | "discount_amount" | "tax_bp" | "billing" | "billing_period" | "term_periods" | "total" | "contract_value"
>;

/** "2 × $600.00", "10 % off", "Tax 8.25 %". */
export function lineFacts(it: LineLike, currency: string): string[] {
  const out = [`${quantityText(it.quantity)} × ${money(it.unit_price, currency)}`];
  const d = discountText(it, currency);
  if (d) out.push(`${d} off`);
  if (it.tax_bp > 0) out.push(`Tax ${crmPercent(it.tax_bp)}`);
  return out;
}

function discountText(it: Pick<LineLike, "discount_bp" | "discount_amount">, currency: string): string | null {
  if (it.discount_bp > 0) return crmPercent(it.discount_bp);
  if (it.discount_amount > 0) return money(it.discount_amount, currency);
  return null;
}

/** A recurring line's schedule: "Monthly for 1 year · $14,400.00"; null for one-time lines. */
export function lineTerm(it: LineLike, currency: string): string | null {
  if (it.billing !== "recurring" || !it.billing_period) return null;
  const head = `${CRM_BILLING_PERIOD_LABEL[it.billing_period].every} for ${termDuration(it.billing_period, it.term_periods)}`;
  return it.term_periods > 1 ? `${head} · ${money(it.contract_value, currency)}` : head;
}

// ---- totals ------------------------------------------------------------------------------

function TotalRow({ label, value, info, strong, sub }: { label: string; value: string; info?: string; strong?: boolean; sub?: boolean }) {
  return (
    <div className={cn("flex items-baseline justify-between gap-4", strong && "border-t border-border pt-2", sub && "pl-3")}>
      <dt className={cn("flex items-center gap-1", strong ? "font-semibold text-fg" : "text-fg-muted")}>
        {label}
        {info ? <InfoTip content={info} className="print:hidden" /> : null}
      </dt>
      <dd className={cn("shrink-0 tabular-nums", strong ? "font-semibold text-fg" : "text-fg")}>{value}</dd>
    </div>
  );
}

/** Subtotal, discount, tax, total; for recurring lines also the one-time part, each schedule and the contract value. */
export function TotalsList({ totals, currency, className }: { totals: CrmTotals; currency: string; className?: string }) {
  const recurring = totals.recurring.length > 0;
  return (
    <dl className={cn("flex flex-col gap-1 type-label", className)}>
      <TotalRow label="Subtotal" value={money(totals.subtotal, currency)} />
      {totals.discount > 0 ? <TotalRow label="Discount" value={`−${money(totals.discount, currency)}`} /> : null}
      {totals.tax > 0 ? <TotalRow label="Tax" value={money(totals.tax, currency)} /> : null}
      <TotalRow strong label="Total" value={money(totals.total, currency)} {...(recurring ? { info: "One-time lines plus the first billing period of each recurring line." } : {})} />
      {recurring && totals.one_time > 0 ? <TotalRow sub label="One-time" value={money(totals.one_time, currency)} /> : null}
      {totals.recurring.map((r) => (
        <TotalRow key={r.billing_period} sub label={CRM_BILLING_PERIOD_LABEL[r.billing_period].every} value={`${money(r.total, currency)} ${CRM_BILLING_PERIOD_LABEL[r.billing_period].per}`} />
      ))}
      {totals.contract_value !== totals.total ? (
        <TotalRow strong label="Contract value" value={money(totals.contract_value, currency)} info="One-time lines plus each recurring line for its whole term." />
      ) : null}
    </dl>
  );
}

// ---- the document -----------------------------------------------------------------------

/** What the printable quote shows (a published quote, the buyer's page, or a draft's preview). */
export interface QuoteDocumentData {
  number: string;
  title: string;
  currency: string;
  seller_name: string;
  sender_name: string;
  sender_email: string;
  buyer_name: string;
  buyer_company: string;
  published_at: string | null;
  expires_at: string;
  items: CrmQuoteItem[];
  totals: CrmTotals;
  comments: string;
  terms: string;
}

function Party({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <h2 className="type-caption font-semibold text-fg-muted">{title}</h2>
      <div className="flex flex-col type-body text-fg [overflow-wrap:anywhere]">{children}</div>
    </div>
  );
}

/** Items as a table (≥ 640 px and in print) or stacked rows (phones). */
export function QuoteItemsTable({ items, currency }: { items: CrmQuoteItem[]; currency: string }) {
  if (items.length === 0) return <p className="type-label text-fg-muted">No lines yet</p>;
  const anyDiscount = items.some((it) => it.discount_bp > 0 || it.discount_amount > 0);
  const anyTax = items.some((it) => it.tax_bp > 0);
  return (
    <>
      <table className="hidden w-full border-collapse sm:table print:table">
        <thead>
          <tr className="border-b border-border-strong type-caption text-fg-muted">
            <th scope="col" className="py-2 pr-4 text-left font-semibold">
              Item
            </th>
            <th scope="col" className="px-2 py-2 text-right font-semibold">
              Qty
            </th>
            <th scope="col" className="px-2 py-2 text-right font-semibold">
              Unit price
            </th>
            {anyDiscount ? (
              <th scope="col" className="px-2 py-2 text-right font-semibold">
                Discount
              </th>
            ) : null}
            {anyTax ? (
              <th scope="col" className="px-2 py-2 text-right font-semibold">
                Tax
              </th>
            ) : null}
            <th scope="col" className="py-2 pl-4 text-right font-semibold">
              Amount
            </th>
          </tr>
        </thead>
        <tbody>
          {items.map((it) => {
            const term = lineTerm(it, currency);
            return (
              <tr key={it.id} className="break-inside-avoid border-b border-border align-top type-label text-fg">
                <td className="py-3 pr-4">
                  <div className="flex flex-col gap-0.5">
                    <span className="font-semibold [overflow-wrap:anywhere]">{it.name}</span>
                    {it.sku ? <span className="type-caption text-fg-muted">{it.sku}</span> : null}
                    {it.description ? <span className="whitespace-pre-wrap type-caption text-fg-muted">{it.description}</span> : null}
                    {term ? <span className="type-caption text-fg-muted">{term}</span> : null}
                  </div>
                </td>
                <td className="px-2 py-3 text-right tabular-nums">{quantityText(it.quantity)}</td>
                <td className="px-2 py-3 text-right tabular-nums whitespace-nowrap">{money(it.unit_price, currency)}</td>
                {anyDiscount ? <td className="px-2 py-3 text-right tabular-nums whitespace-nowrap">{discountText(it, currency) ?? "—"}</td> : null}
                {anyTax ? <td className="px-2 py-3 text-right tabular-nums whitespace-nowrap">{it.tax_bp > 0 ? crmPercent(it.tax_bp) : "—"}</td> : null}
                <td className="py-3 pl-4 text-right tabular-nums whitespace-nowrap font-semibold">{priceText(it.total, currency, it.billing, it.billing_period)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <ul className="flex flex-col divide-y divide-border border-y border-border sm:hidden print:hidden">
        {items.map((it) => {
          const term = lineTerm(it, currency);
          return (
            <li key={it.id} className="flex flex-col gap-1 py-3">
              <div className="flex items-baseline justify-between gap-3 type-label">
                <span className="min-w-0 font-semibold text-fg [overflow-wrap:anywhere]">{it.name}</span>
                <span className="shrink-0 font-semibold tabular-nums text-fg">{priceText(it.total, currency, it.billing, it.billing_period)}</span>
              </div>
              <p className="type-caption text-fg-muted">{lineFacts(it, currency).join(" · ")}</p>
              {it.description ? <p className="whitespace-pre-wrap type-caption text-fg-muted">{it.description}</p> : null}
              {term ? <p className="type-caption text-fg-muted">{term}</p> : null}
            </li>
          );
        })}
      </ul>
    </>
  );
}

/** The quote as a document: seller, title, number and dates, parties, comments, items, totals, terms. */
export function QuoteDocument({ quote: q, className }: { quote: QuoteDocumentData; className?: string }) {
  return (
    <div className={cn("flex flex-col gap-8", className)}>
      <header className="flex flex-col gap-6 sm:flex-row sm:items-start sm:justify-between print:flex-row print:justify-between">
        <div className="flex min-w-0 flex-col gap-1">
          {q.seller_name ? <p className="type-label font-semibold text-fg-muted">{q.seller_name}</p> : null}
          <h1 className="type-headline text-fg [overflow-wrap:anywhere]">{q.title}</h1>
        </div>
        <dl className="grid shrink-0 grid-cols-[auto_auto] justify-start gap-x-4 gap-y-1 type-label sm:justify-end print:justify-end">
          <dt className="text-fg-muted">Quote</dt>
          <dd className="font-semibold tabular-nums text-fg sm:text-right">{q.number}</dd>
          {q.published_at ? (
            <>
              <dt className="text-fg-muted">Date</dt>
              <dd className="tabular-nums text-fg sm:text-right">{formatDate(q.published_at)}</dd>
            </>
          ) : null}
          <dt className="text-fg-muted">Valid until</dt>
          <dd className="tabular-nums text-fg sm:text-right">{formatDate(q.expires_at)}</dd>
        </dl>
      </header>
      {q.buyer_name || q.buyer_company || q.sender_name ? (
        <div className="grid gap-6 sm:grid-cols-2 print:grid-cols-2">
          {q.buyer_name || q.buyer_company ? (
            <Party title="Prepared for">
              {q.buyer_name ? <span>{q.buyer_name}</span> : null}
              {q.buyer_company ? <span className="text-fg-muted">{q.buyer_company}</span> : null}
            </Party>
          ) : null}
          {q.sender_name ? (
            <Party title="From">
              <span>{q.sender_name}</span>
              {q.sender_email ? (
                <a href={`mailto:${q.sender_email}`} className="text-fg-muted underline-offset-4 hover:underline">
                  {q.sender_email}
                </a>
              ) : null}
              {q.seller_name ? <span className="text-fg-muted">{q.seller_name}</span> : null}
            </Party>
          ) : null}
        </div>
      ) : null}
      {q.comments ? <p className="max-w-prose whitespace-pre-wrap type-body text-fg">{q.comments}</p> : null}
      <section aria-label="Items" className="flex flex-col gap-6">
        <QuoteItemsTable items={q.items} currency={q.currency} />
        {q.items.length > 0 ? <TotalsList totals={q.totals} currency={q.currency} className="w-full break-inside-avoid sm:ml-auto sm:max-w-sm print:ml-auto print:max-w-sm" /> : null}
      </section>
      {q.terms ? (
        <section className="flex break-inside-avoid flex-col gap-2">
          <h2 className="type-title-sm text-fg">Terms</h2>
          <p className="max-w-prose whitespace-pre-wrap type-label text-fg-muted">{q.terms}</p>
        </section>
      ) : null}
    </div>
  );
}

// ---- printing ---------------------------------------------------------------------------

// While a screen that renders this is mounted, printing shows only the element marked
// data-print-root: everything that neither is it, holds it nor sits inside it is left out
// (the app's navigation, the CRM header, dialogs, toasts), and the containers around it
// drop their padding so the document fills the page.
const PRINT_CSS = `@page { margin: 16mm; }
body *:not(:has([data-print-root])):not([data-print-root]):not([data-print-root] *) { display: none !important; }
body *:has([data-print-root]) { padding: 0 !important; margin: 0 !important; border: 0 !important; min-height: 0 !important; max-width: none !important; gap: 0 !important; box-shadow: none !important; }
[data-print-root] { max-width: none !important; padding: 0 !important; }`;

/** Print only the element marked `data-print-root`. Render once per screen. */
export function QuotePrintStyles() {
  return <style media="print">{PRINT_CSS}</style>;
}

/**
 * Prints in light colours even when the screen is dark (paper is white and browsers drop
 * backgrounds): the element takes the light theme for the length of the print.
 */
export function usePrintInLight(ref: RefObject<HTMLElement | null>) {
  useEffect(() => {
    let previous: string | null = null;
    const before = () => {
      const el = ref.current;
      if (!el) return;
      previous = el.getAttribute("data-theme");
      el.setAttribute("data-theme", "light");
    };
    const after = () => {
      const el = ref.current;
      if (!el) return;
      if (previous === null) el.removeAttribute("data-theme");
      else el.setAttribute("data-theme", previous);
    };
    window.addEventListener("beforeprint", before);
    window.addEventListener("afterprint", after);
    return () => {
      window.removeEventListener("beforeprint", before);
      window.removeEventListener("afterprint", after);
    };
  }, [ref]);
}
