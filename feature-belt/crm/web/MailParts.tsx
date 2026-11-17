"use client";

import { connectionFixPath, errorMessage, isApiError, isConnectionErrorCode, type CrmMailAccount, type CrmMergeField } from "@scaffoldapp/shared";
import { Banner, Button, Menu, MenuItem, MenuLabel, ProviderIcon, controlClasses, cn, useFieldControlProps } from "@scaffoldapp/ui-web";
import { Braces, X } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useRef, useState, type ClipboardEvent, type KeyboardEvent, type RefObject } from "react";

/** A merge field as the backend reads it: {{contact.first_name}} or {{contact.first_name | fallback}}. */
const MERGE_RE = /\{\{\s*([a-z_]+(?:\.[a-z_]+)?)\s*(?:\|[^{}]*?)?\}\}/g;

/** True when text still has merge fields to fill. */
export function hasMergeFields(text: string): boolean {
  MERGE_RE.lastIndex = 0;
  return MERGE_RE.test(text);
}

/** Merge field keys in text (in order, unique). */
export function mergeKeys(text: string): string[] {
  const out: string[] = [];
  for (const m of text.matchAll(MERGE_RE)) if (m[1] && !out.includes(m[1])) out.push(m[1]);
  return out;
}

/** Labels for merge field keys ("Contact first name"), falling back to {{key}}. */
export function mergeLabels(keys: string[], fields: CrmMergeField[] | undefined): string {
  return keys.map((k) => fields?.find((f) => f.key === k)?.label ?? `{{${k}}}`).join(", ");
}

export const EMAIL_RE = /^[^\s@,;<>]+@[^\s@,;<>]+\.[^\s@,;<>]+$/;

/** Account label for selects: the address, with the platform. */
export function mailAccountLabel(a: CrmMailAccount): string {
  return a.account || (a.provider === "google" ? "Gmail" : "Outlook");
}

export function mailProviderName(provider: string): string {
  return provider === "google" ? "Gmail" : provider === "microsoft" ? "Outlook" : provider;
}

export function MailAccountIcon({ provider }: { provider: string }) {
  return <ProviderIcon provider={provider} size={16} />;
}

/** Where a connection error is fixed: the error's own fix_url, else Settings → Connections for the capability. */
export function connectionFix(error: unknown, capability: string | null, returnTo: string): { href: string; label: string } | null {
  if (!isApiError(error) || !isConnectionErrorCode(error.code)) return null;
  if (error.code === "connection_rate_limited" || error.code === "connection_unavailable" || error.code === "connection_blocked") return null;
  const fix = typeof error.details?.["fix_url"] === "string" ? (error.details["fix_url"] as string) : null;
  const base = fix && fix.startsWith("/") && !fix.startsWith("//") ? fix : connectionFixPath(capability, null);
  const href = `${base}${base.includes("?") ? "&" : "?"}return_to=${encodeURIComponent(returnTo)}`;
  return { href, label: error.code === "connection_needs_auth" ? "Reconnect" : "Open Connections" };
}

/** An error from a mailbox or the CRM (or a sentence of the screen's own), with the fix when the connection needs one. */
export function MailErrorBanner({ error, capability, className }: { error: unknown; capability: string | null; className?: string }) {
  const pathname = usePathname() ?? "/crm";
  const fix = connectionFix(error, capability, pathname);
  const message = typeof error === "string" ? error : errorMessage(error, "Something went wrong. Try again.");
  return (
    <Banner
      tone="danger"
      className={className}
      action={
        fix ? (
          <Button asChild variant="outline" size="sm">
            <Link href={fix.href}>{fix.label}</Link>
          </Button>
        ) : undefined
      }
    >
      {message}
    </Banner>
  );
}

/**
 * Insert text at the caret of an input or textarea (or at the end) and return the new value
 * and caret position.
 */
export function insertAtCaret(el: HTMLInputElement | HTMLTextAreaElement | null, value: string, text: string): { value: string; caret: number } {
  const start = el && typeof el.selectionStart === "number" ? el.selectionStart : value.length;
  const end = el && typeof el.selectionEnd === "number" ? el.selectionEnd : value.length;
  const next = value.slice(0, start) + text + value.slice(end);
  return { value: next, caret: start + text.length };
}

/**
 * Inserting from a menu: Radix gives focus back to the menu's trigger when it closes, so the
 * trigger's onFocus hands it on to the field the text went into (with a fallback timer).
 */
export function useCaretInsert() {
  const pending = useRef<{ el: HTMLInputElement | HTMLTextAreaElement; caret: number } | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const settle = () => {
    const p = pending.current;
    pending.current = null;
    if (timer.current) clearTimeout(timer.current);
    if (!p) return;
    p.el.focus();
    try {
      p.el.setSelectionRange(p.caret, p.caret);
    } catch {
      // Inputs of some types don't take a selection.
    }
  };
  return {
    /** Call after setting the new value. */
    focusAfter(el: HTMLInputElement | HTMLTextAreaElement | null, caret: number) {
      if (!el) return;
      pending.current = { el, caret };
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(settle, 320);
    },
    /** Put on the menu trigger. */
    onTriggerFocus: () => {
      if (pending.current) setTimeout(settle, 0);
    },
  };
}

/** "Merge field" menu: inserts {{key}} where the caret is. */
export function MergeFieldMenu({
  fields,
  onInsert,
  onTriggerFocus,
  disabled,
  size = "sm",
}: {
  fields: CrmMergeField[] | undefined;
  onInsert: (key: string) => void;
  onTriggerFocus?: () => void;
  disabled?: boolean;
  size?: "sm" | "md";
}) {
  const groups = groupMergeFields(fields ?? []);
  return (
    <Menu
      align="start"
      className="max-h-80 overflow-y-auto"
      trigger={
        <Button variant="ghost" size={size} leftIcon={<Braces />} disabled={disabled || !fields?.length} onFocus={onTriggerFocus}>
          Merge field
        </Button>
      }
    >
      {groups.map((g) => (
        <div key={g.label} role="group" aria-label={g.label}>
          <MenuLabel>{g.label}</MenuLabel>
          {g.fields.map((f) => (
            <MenuItem key={f.key} onSelect={() => onInsert(f.key)}>
              {f.label}
            </MenuItem>
          ))}
        </div>
      ))}
    </Menu>
  );
}

const GROUP_LABEL: Record<string, string> = { contact: "Contact", company: "Company", deal: "Deal", ticket: "Ticket", sender: "You", booking_link: "You" };

function groupMergeFields(fields: CrmMergeField[]): { label: string; fields: CrmMergeField[] }[] {
  const out: { label: string; fields: CrmMergeField[] }[] = [];
  for (const f of fields) {
    const label = GROUP_LABEL[f.key.split(".")[0] ?? ""] ?? "Other";
    let g = out.find((x) => x.label === label);
    if (!g) out.push((g = { label, fields: [] }));
    g.fields.push(f);
  }
  return out;
}

/**
 * Email addresses as removable chips: type and press Enter, comma or Tab to add one;
 * Backspace on an empty input removes the last; pasting a list splits it.
 */
export function AddressInput({
  value,
  onChange,
  placeholder,
  inputRef,
  disabled,
}: {
  value: string[];
  onChange: (next: string[]) => void;
  placeholder?: string;
  inputRef?: RefObject<HTMLInputElement | null>;
  disabled?: boolean;
}) {
  const p = useFieldControlProps<{ id?: string | undefined; disabled?: boolean | undefined }>({ disabled });
  const [text, setText] = useState("");
  const add = (raw: string) => {
    const parts = raw
      .split(/[\s,;]+/)
      .map((s) => s.trim().replace(/^<|>$/g, ""))
      .filter(Boolean);
    if (parts.length === 0) return;
    const next = [...value];
    for (const s of parts) if (!next.some((v) => v.toLowerCase() === s.toLowerCase())) next.push(s);
    onChange(next);
  };
  const commit = () => {
    if (!text.trim()) return;
    add(text);
    setText("");
  };
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if ((e.key === "Enter" || e.key === "," || e.key === ";" || (e.key === "Tab" && text.trim())) && !e.metaKey && !e.ctrlKey) {
      if (!text.trim()) return;
      e.preventDefault();
      commit();
    } else if (e.key === "Backspace" && !text && value.length > 0) {
      e.preventDefault();
      onChange(value.slice(0, -1));
    }
  };
  const onPaste = (e: ClipboardEvent<HTMLInputElement>) => {
    const pasted = e.clipboardData.getData("text");
    if (/[\s,;]/.test(pasted.trim())) {
      e.preventDefault();
      add(text + pasted);
      setText("");
    }
  };
  return (
    <div
      className={cn(controlClasses, "flex min-h-control flex-wrap items-center gap-1 px-2 py-1 focus-within:border-ring focus-within:ring-1 focus-within:ring-ring pointer-coarse:min-h-tap", p.disabled && "opacity-50")}
      aria-invalid={p["aria-invalid"]}
      onClick={() => inputRef?.current?.focus()}
    >
      {value.map((addr) => {
        const bad = !EMAIL_RE.test(addr);
        return (
          <span
            key={addr}
            className={cn("inline-flex h-control-sm max-w-full items-center gap-1 rounded-sm pl-2 type-label", bad ? "bg-danger-soft text-danger-strong" : "bg-sunken text-fg")}
          >
            <span className="truncate">{addr}</span>
            {bad ? <span className="sr-only">(not a valid address)</span> : null}
            <button
              type="button"
              disabled={p.disabled}
              aria-label={`Remove ${addr}`}
              onClick={(e) => {
                e.stopPropagation();
                onChange(value.filter((v) => v !== addr));
              }}
              className="relative inline-flex size-6 shrink-0 items-center justify-center rounded-xs text-fg-muted hover:bg-fg/8 hover:text-fg focus-visible:outline-2 focus-visible:outline-ring before:absolute before:-inset-1 before:content-['']"
            >
              <X className="size-4" aria-hidden />
            </button>
          </span>
        );
      })}
      <input
        ref={inputRef}
        id={p.id}
        aria-describedby={p["aria-describedby"]}
        disabled={p.disabled}
        type="text"
        inputMode="email"
        autoComplete="off"
        autoCapitalize="off"
        spellCheck={false}
        value={text}
        placeholder={value.length === 0 ? placeholder : undefined}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={onKeyDown}
        onPaste={onPaste}
        onBlur={commit}
        className="h-control-sm min-w-32 flex-1 bg-transparent px-1 text-body text-fg outline-none placeholder:text-fg-subtle"
      />
    </div>
  );
}
