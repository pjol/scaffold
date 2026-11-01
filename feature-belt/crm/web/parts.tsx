"use client";

import {
  CRM_PATH,
  CRM_TYPE_LABEL,
  crmMoney,
  errorMessage,
  useAccessDirectory,
  useCrmSearch,
  type CrmObjectType,
  type CrmSearchHit,
  type CrmStageOutcome,
  type CrmTag,
  type CrmUserRef,
} from "@scaffoldapp/shared";
import { Badge, Input, Spinner, cn, type BadgeTone } from "@scaffoldapp/ui-web";
import { Building2, Handshake, Search, UserRound } from "lucide-react";
import Link from "next/link";
import { useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";

export const TYPE_ICON: Record<CrmObjectType, typeof Building2> = { company: Building2, contact: UserRound, deal: Handshake };

export function recordHref(type: CrmObjectType, id: string): string {
  return `/crm/${CRM_PATH[type]}/${id}`;
}

/** An amount in its currency, tabular. */
export function MoneyText({ minor, currency, short, className }: { minor: number | null | undefined; currency: string; short?: boolean; className?: string }) {
  if (minor === null || minor === undefined) return <span className={cn("text-fg-subtle", className)}>—</span>;
  return <span className={cn("tabular-nums", className)}>{crmMoney(minor, currency, { short: !!short })}</span>;
}

export const OUTCOME_TONE: Record<CrmStageOutcome, BadgeTone> = { open: "neutral", won: "success", lost: "danger" };

export function StageBadge({ name, outcome }: { name: string; outcome: CrmStageOutcome }) {
  return (
    <Badge tone={OUTCOME_TONE[outcome]} dot={outcome !== "open"}>
      {name}
    </Badge>
  );
}

export function TagList({ tags, max = 3 }: { tags: CrmTag[]; max?: number }) {
  if (tags.length === 0) return null;
  const shown = tags.slice(0, max);
  return (
    <span className="flex min-w-0 flex-wrap items-center gap-1">
      {shown.map((t) => (
        <Badge key={t.id} tone={t.tone}>
          {t.name}
        </Badge>
      ))}
      {tags.length > max ? <Badge>+{tags.length - max}</Badge> : null}
    </span>
  );
}

export function PersonChip({ user, size = "xs" }: { user: CrmUserRef | null; size?: "xs" | "sm" }) {
  if (!user) return <span className="text-fg-subtle">Unassigned</span>;
  return (
    <span className="inline-flex min-w-0 items-center gap-2">
      <AppAvatar name={user.name} src={user.avatar_url} size={size} />
      <span className="truncate">{user.name}</span>
    </span>
  );
}

/** "Today", "Tomorrow", "Mon, Nov 3" or "Nov 3, 2027" for a YYYY-MM-DD or ISO date. */
export function dayLabel(value: string, now = new Date()): string {
  const d = value.length === 10 ? new Date(`${value}T12:00:00`) : new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  const start = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((start(d) - start(now)) / 86_400_000);
  if (diff === 0) return "Today";
  if (diff === 1) return "Tomorrow";
  if (diff === -1) return "Yesterday";
  const sameYear = d.getFullYear() === now.getFullYear();
  return d.toLocaleDateString(undefined, sameYear ? { weekday: Math.abs(diff) < 7 ? "short" : undefined, month: "short", day: "numeric" } : { month: "short", day: "numeric", year: "numeric" });
}

export function timeLabel(iso: string): string {
  return new Date(iso).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
}

/**
 * Search records of some types and pick one (combobox: arrow keys, Enter, Escape).
 * Results open below the input in the page flow, so it works inside dialogs.
 */
export function RecordPicker({
  orgId,
  types,
  exclude,
  onPick,
  label,
  placeholder,
  autoFocus,
}: {
  orgId: string;
  types: CrmObjectType[];
  exclude?: ReadonlySet<string>;
  onPick: (hit: CrmSearchHit) => void;
  label: string;
  placeholder?: string;
  autoFocus?: boolean;
}) {
  const listId = useId();
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const search = useCrmSearch(orgId, q, types, { limit: 8, enabled: open });
  const items = useMemo(() => (search.data ?? []).filter((h) => !exclude?.has(h.id)), [search.data, exclude]);
  const pick = (h: CrmSearchHit) => {
    onPick(h);
    setQ("");
    setActive(0);
  };
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setOpen(true);
      setActive((a) => Math.min(a + 1, items.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((a) => Math.max(a - 1, 0));
    } else if (e.key === "Enter" && open && items[active]) {
      e.preventDefault();
      pick(items[active]);
    } else if (e.key === "Escape") {
      setOpen(false);
    }
  };
  return (
    <div className="flex flex-col gap-2">
      <Input
        ref={inputRef}
        role="combobox"
        aria-label={label}
        aria-expanded={open}
        aria-controls={listId}
        aria-activedescendant={open && items[active] ? `${listId}-${items[active].id}` : undefined}
        placeholder={placeholder ?? "Search by name, email or domain"}
        leadingIcon={<Search aria-hidden />}
        trailing={search.isFetching ? <Spinner size={16} label="Searching" /> : undefined}
        value={q}
        autoFocus={autoFocus}
        onChange={(e) => {
          setQ(e.target.value);
          setOpen(true);
          setActive(0);
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setTimeout(() => setOpen(false), 150)}
        onKeyDown={onKeyDown}
      />
      {open ? (
        <ul id={listId} role="listbox" aria-label={label} className="flex max-h-72 flex-col overflow-y-auto rounded-md border border-border bg-surface p-1">
          {search.isError ? (
            <li className="px-3 py-2 type-label text-danger-strong">{errorMessage(search.error, "Couldn't search.")}</li>
          ) : items.length === 0 ? (
            <li className="px-3 py-2 type-label text-fg-muted">{search.isPending ? "Searching…" : q ? `Nothing matches “${q}”` : "Type to search"}</li>
          ) : (
            items.map((h, i) => {
              const Icon = TYPE_ICON[h.type];
              return (
                <li
                  key={h.id}
                  id={`${listId}-${h.id}`}
                  role="option"
                  aria-selected={i === active}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => pick(h)}
                  onMouseEnter={() => setActive(i)}
                  className={cn("flex min-h-control cursor-pointer items-center gap-3 rounded-sm px-3 py-1.5", i === active && "bg-sunken")}
                >
                  <Icon className="size-4 shrink-0 text-fg-muted" aria-hidden />
                  <span className="min-w-0 flex-1 truncate type-label text-fg">{h.title}</span>
                  {h.detail ? <span className="hidden shrink-0 truncate type-caption text-fg-muted sm:inline">{h.detail}</span> : null}
                  {types.length > 1 ? <span className="shrink-0 type-caption text-fg-subtle">{CRM_TYPE_LABEL[h.type].One}</span> : null}
                </li>
              );
            })
          )}
        </ul>
      ) : null}
    </div>
  );
}

/** Pick a member of the org (combobox over the access directory). */
export function MemberPicker({ orgId, onPick, label, autoFocus }: { orgId: string; onPick: (u: CrmUserRef) => void; label: string; autoFocus?: boolean }) {
  const [q, setQ] = useState("");
  const dir = useAccessDirectory(orgId, q, { types: "user", limit: 8 });
  return (
    <div className="flex flex-col gap-2">
      <Input aria-label={label} placeholder="Search people" leadingIcon={<Search aria-hidden />} value={q} autoFocus={autoFocus} onChange={(e) => setQ(e.target.value)} />
      <ul className="flex max-h-64 flex-col overflow-y-auto" aria-label={label}>
        {(dir.data ?? []).map((p) => (
          <li key={p.id}>
            <button
              type="button"
              onClick={() => onPick({ id: p.id, name: p.name, avatar_url: p.avatar_url })}
              className="flex min-h-control w-full items-center gap-3 rounded-sm px-3 py-1.5 text-left hover:bg-sunken focus-visible:bg-sunken"
            >
              <AppAvatar name={p.name} src={p.avatar_url} size="xs" />
              <span className="min-w-0 flex-1 truncate type-label text-fg">{p.name}</span>
              {p.email ? <span className="truncate type-caption text-fg-muted">{p.email}</span> : null}
            </button>
          </li>
        ))}
        {dir.data && dir.data.length === 0 ? <li className="px-3 py-2 type-label text-fg-muted">No one matches “{q}”</li> : null}
      </ul>
    </div>
  );
}

/** A record link with its type icon. */
export function RecordLink({ type, id, name, className }: { type: CrmObjectType; id: string; name: string; className?: string }) {
  const Icon = TYPE_ICON[type];
  return (
    <Link href={recordHref(type, id)} className={cn("inline-flex min-w-0 items-center gap-1.5 font-semibold text-fg hover:underline", className)}>
      <Icon className="size-4 shrink-0 text-fg-muted" aria-hidden />
      <span className="truncate">{name}</span>
    </Link>
  );
}
