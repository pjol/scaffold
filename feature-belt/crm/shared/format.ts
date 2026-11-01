import type {
  CrmActivity,
  CrmActivityType,
  CrmFilter,
  CrmFilterOp,
  CrmLifecycleStage,
  CrmMention,
  CrmMoneyTotal,
  CrmObjectType,
  CrmPriority,
  CrmProperty,
  CrmPropertyType,
  CrmRecord,
  CrmRecordAccess,
  CrmTagTone,
} from "./types";

export const CRM_TYPE_LABEL: Record<CrmObjectType, { one: string; many: string; One: string; Many: string }> = {
  company: { one: "company", many: "companies", One: "Company", Many: "Companies" },
  contact: { one: "contact", many: "contacts", One: "Contact", Many: "Contacts" },
  deal: { one: "deal", many: "deals", One: "Deal", Many: "Deals" },
};

export const CRM_LIFECYCLE_LABEL: Record<CrmLifecycleStage, string> = {
  subscriber: "Subscriber",
  lead: "Lead",
  mql: "Marketing qualified lead",
  sql: "Sales qualified lead",
  opportunity: "Opportunity",
  customer: "Customer",
  evangelist: "Evangelist",
  other: "Other",
};

export const CRM_ACTIVITY_LABEL: Record<CrmActivityType, string> = { note: "Note", call: "Call", email: "Email", meeting: "Meeting", task: "Task" };

export const CRM_CALL_OUTCOMES: { value: string; label: string }[] = [
  { value: "connected", label: "Connected" },
  { value: "no_answer", label: "No answer" },
  { value: "left_voicemail", label: "Left voicemail" },
  { value: "busy", label: "Busy" },
  { value: "wrong_number", label: "Wrong number" },
];

export const CRM_MEETING_OUTCOMES: { value: string; label: string }[] = [
  { value: "scheduled", label: "Scheduled" },
  { value: "completed", label: "Completed" },
  { value: "rescheduled", label: "Rescheduled" },
  { value: "no_show", label: "No show" },
  { value: "canceled", label: "Canceled" },
];

/** Label of a call or meeting outcome. */
export function crmOutcomeLabel(outcome: string | null | undefined): string {
  if (!outcome) return "";
  return [...CRM_CALL_OUTCOMES, ...CRM_MEETING_OUTCOMES].find((o) => o.value === outcome)?.label ?? outcome;
}

export const CRM_PRIORITY_LABEL: Record<CrmPriority, string> = { none: "No priority", low: "Low", medium: "Medium", high: "High" };

export const CRM_RECORD_ACCESS: { value: CrmRecordAccess; label: string; info: string }[] = [
  { value: "everyone_edits", label: "Everyone edits", info: "Everyone who uses the CRM can see and change every record." },
  {
    value: "everyone_views",
    label: "Everyone views, owners edit",
    info: "Everyone sees every record and can log activity on it. Only the owner, people it's shared with and admins change it.",
  },
  {
    value: "owners_teams",
    label: "Owners and their teams only",
    info: "A record is visible only to its owner, the members of its team, people it's shared with and admins. Totals and searches leave out what someone can't see.",
  },
];

export const CRM_TAG_TONES: readonly CrmTagTone[] = ["neutral", "primary", "accent", "success", "warning", "danger", "info"];

// ---- money ---------------------------------------------------------------------------

const ZERO_DECIMALS = new Set(["BIF", "CLP", "DJF", "GNF", "ISK", "JPY", "KMF", "KRW", "PYG", "RWF", "UGX", "VND", "VUV", "XAF", "XOF", "XPF"]);
const THREE_DECIMALS = new Set(["BHD", "IQD", "JOD", "KWD", "LYD", "OMR", "TND"]);

/** Minor-unit digits of a currency (ISO 4217), matching the server. */
export function crmCurrencyExponent(currency: string): number {
  if (ZERO_DECIMALS.has(currency)) return 0;
  if (THREE_DECIMALS.has(currency)) return 3;
  return 2;
}

/** Format minor units: "$28,800" (compact drops cents on whole amounts; `short` gives "$28.8K"). */
export function crmMoney(minor: number | null | undefined, currency: string, opts: { short?: boolean; cents?: boolean } = {}): string {
  if (minor === null || minor === undefined) return "";
  const exp = crmCurrencyExponent(currency);
  const major = minor / 10 ** exp;
  const whole = minor % 10 ** exp === 0;
  try {
    return new Intl.NumberFormat(undefined, {
      style: "currency",
      currency,
      notation: opts.short ? "compact" : "standard",
      maximumFractionDigits: opts.short ? 1 : opts.cents || !whole ? exp : 0,
      minimumFractionDigits: opts.short || (!opts.cents && whole) ? 0 : exp,
    }).format(major);
  } catch {
    return `${major.toFixed(exp)} ${currency}`;
  }
}

/** Totals in several currencies, the main currency first: "$12,400 + €3,000". */
export function crmMoneyTotals(totals: CrmMoneyTotal[], main: string, field: "amount" | "weighted" = "amount", opts: { short?: boolean } = {}): string {
  if (totals.length === 0) return crmMoney(0, main, opts);
  const sorted = [...totals].sort((a, b) => (a.currency === main ? -1 : b.currency === main ? 1 : a.currency.localeCompare(b.currency)));
  return sorted.map((t) => crmMoney(t[field], t.currency, opts)).join(" + ");
}

/** The sum in the main currency only (for bars and charts). */
export function crmMainTotal(totals: CrmMoneyTotal[], main: string, field: "amount" | "weighted" = "amount"): number {
  return totals.find((t) => t.currency === main)?.[field] ?? 0;
}

/** Parse what someone typed ("1,250.50", "€900") into minor units; null when it isn't an amount. */
export function crmParseMoney(input: string, currency: string): number | null {
  const s = input.replace(/[^\d.,-]/g, "").trim();
  if (!s || s.includes("-")) return null;
  const exp = crmCurrencyExponent(currency);
  let normalized = s;
  const lastComma = s.lastIndexOf(",");
  const lastDot = s.lastIndexOf(".");
  if (lastComma > lastDot && s.length - lastComma - 1 > 0 && s.length - lastComma - 1 <= exp && s.length - lastComma - 1 !== 3) {
    normalized = s.slice(0, lastComma).replace(/\./g, "") + "." + s.slice(lastComma + 1);
  }
  normalized = normalized.replace(/,/g, "");
  const [whole = "0", frac = ""] = normalized.split(".");
  if (frac.length > exp || !/^\d*$/.test(whole) || !/^\d*$/.test(frac)) return null;
  const n = Number(whole || "0") * 10 ** exp + Number((frac + "0".repeat(exp)).slice(0, exp) || "0");
  return Number.isSafeInteger(n) ? n : null;
}

/** Minor units as an editable decimal string ("1250.5" → "1250.50"). */
export function crmMoneyInput(minor: number | null | undefined, currency: string): string {
  if (minor === null || minor === undefined) return "";
  const exp = crmCurrencyExponent(currency);
  return (minor / 10 ** exp).toFixed(exp);
}

// ---- time ------------------------------------------------------------------------------

/** A duration in the largest sensible unit: "45 min", "6 h", "12 d". */
export function crmDuration(seconds: number): string {
  if (seconds < 3600) return `${Math.max(1, Math.round(seconds / 60))} min`;
  if (seconds < 86400 * 2) return `${Math.round(seconds / 3600)} h`;
  return `${Math.round(seconds / 86400)} d`;
}

/** Whether an open task's due time has passed. */
export function crmIsOverdue(task: Pick<CrmActivity, "due_at" | "done_at">, now = Date.now()): boolean {
  return !task.done_at && !!task.due_at && new Date(task.due_at).getTime() < now;
}

// ---- mentions --------------------------------------------------------------------------

export type CrmBodySegment = { kind: "text"; text: string } | { kind: "mention"; mention: CrmMention | null; raw: string };

const MENTION_RE = /<@(team:)?([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})>/g;

/** Split a body into text and mentions for rendering (an unknown mention has mention: null). */
export function crmBodySegments(body: string, mentions: CrmMention[]): CrmBodySegment[] {
  const out: CrmBodySegment[] = [];
  let last = 0;
  for (const m of body.matchAll(MENTION_RE)) {
    const at = m.index ?? 0;
    if (at > last) out.push({ kind: "text", text: body.slice(last, at) });
    out.push({ kind: "mention", raw: m[0], mention: mentions.find((x) => x.id === m[2]) ?? null });
    last = at + m[0].length;
  }
  if (last < body.length) out.push({ kind: "text", text: body.slice(last) });
  return out;
}

/** The token a composer inserts for a person or team. */
export function crmMentionToken(m: { id: string; type: "user" | "team" }): string {
  return m.type === "team" ? `<@team:${m.id}>` : `<@${m.id}>`;
}

/** A body with mentions written as @names (previews, notifications). */
export function crmPlainBody(body: string, mentions: CrmMention[]): string {
  return crmBodySegments(body, mentions)
    .map((s) => (s.kind === "text" ? s.text : `@${s.mention?.name ?? "someone"}`))
    .join("");
}

// ---- properties and filters --------------------------------------------------------------

/** Conditions a filter offers per property type, with labels. */
export function crmFilterOps(type: CrmPropertyType): { op: CrmFilterOp; label: string }[] {
  switch (type) {
    case "number":
    case "currency":
      return [
        { op: "eq", label: "is" },
        { op: "gt", label: "is more than" },
        { op: "lt", label: "is less than" },
        { op: "gte", label: "is at least" },
        { op: "lte", label: "is at most" },
        { op: "empty", label: "is empty" },
        { op: "not_empty", label: "is not empty" },
      ];
    case "date":
    case "datetime":
      return [
        { op: "on", label: "is on" },
        { op: "before", label: "is before" },
        { op: "after", label: "is after" },
        { op: "last_days", label: "is in the last … days" },
        { op: "next_days", label: "is in the next … days" },
        { op: "empty", label: "is empty" },
        { op: "not_empty", label: "is not empty" },
      ];
    case "select":
    case "user":
    case "team":
    case "stage":
    case "pipeline":
    case "company":
      return [
        { op: "is_any", label: "is any of" },
        { op: "is_none", label: "is none of" },
        { op: "empty", label: "is empty" },
        { op: "not_empty", label: "is not empty" },
      ];
    case "tags":
    case "multi_select":
      return [
        { op: "has_any", label: "has any of" },
        { op: "has_all", label: "has all of" },
        { op: "has_none", label: "has none of" },
        { op: "empty", label: "is empty" },
        { op: "not_empty", label: "is not empty" },
      ];
    case "checkbox":
      return [
        { op: "is_true", label: "is checked" },
        { op: "is_false", label: "is not checked" },
      ];
    case "emails":
    case "phones":
      return [
        { op: "contains", label: "contains" },
        { op: "eq", label: "is" },
        { op: "empty", label: "is empty" },
        { op: "not_empty", label: "is not empty" },
      ];
    default:
      return [
        { op: "contains", label: "contains" },
        { op: "eq", label: "is" },
        { op: "neq", label: "is not" },
        { op: "not_contains", label: "doesn't contain" },
        { op: "empty", label: "is empty" },
        { op: "not_empty", label: "is not empty" },
      ];
  }
}

/** Ops that take no value. */
export function crmOpNeedsValue(op: CrmFilterOp): boolean {
  return !["empty", "not_empty", "is_true", "is_false"].includes(op);
}

/** Whether a filter is complete enough to send. */
export function crmFilterReady(f: CrmFilter): boolean {
  if (!crmOpNeedsValue(f.op)) return true;
  if (Array.isArray(f.value)) return f.value.length > 0;
  return f.value !== undefined && f.value !== "";
}

/** Plain text of a record's field for tables and exports-in-the-UI. */
export function crmFieldText(rec: CrmRecord, prop: CrmProperty, opts: { currency?: string; people?: Record<string, string> } = {}): string {
  const r = rec as unknown as Record<string, unknown>;
  switch (prop.key) {
    case "owner":
      return rec.owner?.name ?? "";
    case "team":
      return rec.team?.name ?? "";
    case "tags":
      return rec.tags.map((t) => t.name).join(", ");
    case "company":
      return rec.type === "company" ? "" : (rec.company?.name ?? "");
    case "stage":
      return rec.type === "deal" ? rec.stage.name : "";
    case "pipeline":
      return rec.type === "deal" ? rec.pipeline_name : "";
    case "probability":
      return rec.type === "deal" ? `${rec.stage.probability}%` : "";
    case "amount":
      return rec.type === "deal" ? crmMoney(rec.amount, rec.currency) : "";
    case "weighted_amount":
      return rec.type === "deal" ? crmMoney(rec.weighted_amount, rec.currency) : "";
    case "lifecycle_stage":
      return rec.type === "contact" ? CRM_LIFECYCLE_LABEL[rec.lifecycle_stage] : "";
    case "emails":
      return rec.type === "contact" ? (rec.emails[0] ?? "") : "";
    case "phones":
      return rec.type === "contact" ? (rec.phones[0] ?? "") : "";
  }
  if (!prop.builtin) {
    const v = rec.props[prop.key];
    if (v === null || v === undefined) return "";
    switch (prop.type) {
      case "select":
        return prop.options.find((o) => o.value === v)?.label ?? String(v);
      case "multi_select":
        return (Array.isArray(v) ? v : []).map((x) => prop.options.find((o) => o.value === x)?.label ?? x).join(", ");
      case "checkbox":
        return v ? "Yes" : "No";
      case "user":
        return rec.props_display[prop.key] ?? opts.people?.[String(v)] ?? "";
      case "currency":
        return typeof v === "number" ? crmMoney(v, opts.currency ?? "USD") : "";
      default:
        return String(v);
    }
  }
  const v = r[prop.key];
  if (v === null || v === undefined) return "";
  return String(v);
}
