"use client";

import { channelsSidebarOrder, useChannelsWorkspace, useEndpoints, channelsEndpoints, useQueryClient, channelsKeys } from "@scaffoldapp/shared";
import { Dialog } from "@scaffoldapp/ui-web";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, useSyncExternalStore } from "react";

let sheetOpen = false;
const listeners = new Set<() => void>();
function setSheet(open: boolean) {
  sheetOpen = open;
  listeners.forEach((l) => l());
}

/** Open the keyboard shortcuts sheet (from a menu). */
export function openShortcutsSheet(): void {
  setSheet(true);
}

const isMac = () => typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

function typing(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  if (!el) return false;
  return el.isContentEditable || el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT";
}

/**
 * Workspace shortcuts: ⌥↑/↓ conversations, ⌥⇧↑/↓ unread ones, ⌘⇧A All unread, ⌘⇧T Threads,
 * ⌘⇧M Activity, Esc marks the open conversation read, ⌘/ shows the sheet.
 */
export function useChannelsShortcuts(orgId: string): void {
  const router = useRouter();
  const pathname = usePathname() ?? "";
  const ws = useChannelsWorkspace(orgId);
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = isMac() ? e.metaKey : e.ctrlKey;
      const current = /^\/channels\/([0-9a-f-]{36})$/.exec(pathname)?.[1] ?? null;
      if (e.altKey && !mod && (e.key === "ArrowUp" || e.key === "ArrowDown")) {
        const data = ws.data;
        if (!data) return;
        let order = channelsSidebarOrder(data.channels, data.prefs.sidebar_sort, data.sections);
        if (e.shiftKey) order = order.filter((x) => (x.unread > 0 && !x.muted) || x.id === current);
        if (order.length === 0) return;
        e.preventDefault();
        const at = order.findIndex((x) => x.id === current);
        const step = e.key === "ArrowUp" ? -1 : 1;
        const next = order[at < 0 ? (step > 0 ? 0 : order.length - 1) : (at + step + order.length) % order.length];
        if (next && next.id !== current) router.push(`/channels/${next.id}`);
        return;
      }
      if (mod && e.shiftKey && !e.altKey) {
        const key = e.key.toLowerCase();
        const to = key === "a" ? "/channels/unreads" : key === "t" ? "/channels/threads" : key === "m" ? "/channels/activity" : null;
        if (to) {
          e.preventDefault();
          router.push(to);
          return;
        }
      }
      if (mod && (e.key === "/" || e.key === "?")) {
        e.preventDefault();
        setSheet(true);
        return;
      }
      if (e.key === "Escape" && current && !e.defaultPrevented && (!typing(e.target) || (e.target as HTMLTextAreaElement).value === "")) {
        void c.markRead(current).then(() => {
          void qc.invalidateQueries({ queryKey: channelsKeys.workspace(orgId) });
          void qc.invalidateQueries({ queryKey: channelsKeys.unread() });
        });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [router, pathname, ws.data, c, qc, orgId]);
}

const SHORTCUTS: { keys: string[]; label: string }[] = [
  { keys: ["⌥", "↑ / ↓"], label: "Previous or next conversation" },
  { keys: ["⌥", "⇧", "↑ / ↓"], label: "Previous or next unread conversation" },
  { keys: ["⌘", "⇧", "A"], label: "All unread" },
  { keys: ["⌘", "⇧", "T"], label: "Threads" },
  { keys: ["⌘", "⇧", "M"], label: "Activity" },
  { keys: ["Esc"], label: "Mark the conversation read" },
  { keys: ["⌘", "K"], label: "Search everything" },
  { keys: ["↑"], label: "Edit your last message (empty composer)" },
  { keys: ["⌘", "B / I"], label: "Bold or italic" },
  { keys: ["⌘", "⇧", "X"], label: "Strikethrough" },
  { keys: ["⌘", "⌥", "C"], label: "Code block" },
  { keys: ["⌘", "⇧", "7 / 8"], label: "Numbered or bulleted list" },
  { keys: ["⌘", "⇧", "9"], label: "Quote" },
  { keys: ["⌘", "⇧", "U"], label: "Link" },
  { keys: ["/"], label: "Commands (start of a message)" },
  { keys: ["⌘", "/"], label: "These shortcuts" },
];

/** The shortcuts sheet (mounted once in the workspace). */
export function ShortcutsSheet() {
  const open = useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => sheetOpen,
    () => false,
  );
  const [mac, setMac] = useState(true);
  useEffect(() => setMac(isMac()), []);
  const key = (k: string) => (mac ? k : k.replace("⌘", "Ctrl").replace("⌥", "Alt").replace("⇧", "Shift"));
  return (
    <Dialog open={open} onOpenChange={setSheet} title="Keyboard shortcuts">
      <dl className="grid grid-cols-[1fr_auto] gap-x-6 gap-y-2">
        {SHORTCUTS.map((s) => (
          <div key={s.label} className="contents">
            <dt className="type-label text-fg">{s.label}</dt>
            <dd className="flex items-center gap-1">
              {s.keys.map((k) => (
                <kbd key={k} className="rounded-xs border border-border bg-sunken px-1.5 py-0.5 font-mono type-caption text-fg-muted">
                  {key(k)}
                </kbd>
              ))}
            </dd>
          </div>
        ))}
      </dl>
    </Dialog>
  );
}
