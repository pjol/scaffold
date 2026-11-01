"use client";

import { cn } from "@scaffoldapp/ui-web";
import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";

export type FloatingAnchor = HTMLElement | DOMRect | { x: number; y: number };

function rectOf(a: FloatingAnchor): DOMRect {
  if (a instanceof HTMLElement) return a.getBoundingClientRect();
  if ("width" in a) return a;
  return new DOMRect(a.x, a.y, 0, 0);
}

export interface FloatingProps {
  open: boolean;
  anchor: FloatingAnchor | null;
  onClose: () => void;
  children: ReactNode;
  placement?: "bottom-start" | "bottom-end" | "top-start" | "right-start";
  className?: string;
  /** Move focus into the panel when it opens (menus, forms); off for caret menus. */
  autoFocus?: boolean;
  label?: string;
  role?: "dialog" | "menu" | "listbox" | "toolbar";
}

/**
 * A panel floating next to an element, a rect (the caret) or a point: portalled, kept in
 * the viewport, closed by Escape or a click outside. Focus returns to where it was.
 */
export function Floating({ open, anchor, onClose, children, placement = "bottom-start", className, autoFocus = false, label, role = "dialog" }: FloatingProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);
  const restore = useRef<HTMLElement | null>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;

  useLayoutEffect(() => {
    if (!open || !anchor) {
      setPos(null);
      return undefined;
    }
    const place = () => {
      const el = ref.current;
      if (!el) return;
      const r = rectOf(anchor);
      const w = el.offsetWidth;
      const h = el.offsetHeight;
      const vw = window.innerWidth;
      const vh = window.innerHeight;
      const gap = 8;
      let top: number;
      let left: number;
      if (placement === "right-start") {
        left = r.right + gap;
        top = r.top;
        if (left + w > vw - gap) left = r.left - w - gap;
      } else {
        left = placement === "bottom-end" ? r.right - w : r.left;
        top = placement === "top-start" ? r.top - h - gap : r.bottom + gap;
        if (placement !== "top-start" && top + h > vh - gap && r.top - h - gap > gap) top = r.top - h - gap;
        if (placement === "top-start" && top < gap) top = r.bottom + gap;
      }
      left = Math.max(gap, Math.min(left, vw - w - gap));
      top = Math.max(gap, Math.min(top, vh - h - gap));
      setPos((p) => (p && p.top === top && p.left === left ? p : { top, left }));
    };
    place();
    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(place) : null;
    if (ref.current) ro?.observe(ref.current);
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      ro?.disconnect();
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [open, anchor, placement]);

  useEffect(() => {
    if (!open) return undefined;
    restore.current = document.activeElement as HTMLElement | null;
    if (autoFocus) {
      requestAnimationFrame(() => {
        const first = ref.current?.querySelector<HTMLElement>("[data-autofocus], input, textarea, button, [tabindex='0']");
        first?.focus();
      });
    }
    const onDown = (e: PointerEvent) => {
      const t = e.target as Node;
      if (ref.current?.contains(t)) return;
      if (anchor instanceof HTMLElement && anchor.contains(t)) return;
      // Clicks inside other floating layers (a select inside the panel) don't count.
      if ((t as HTMLElement).closest?.("[data-floating], [data-radix-popper-content-wrapper]")) return;
      closeRef.current();
    };
    document.addEventListener("pointerdown", onDown, true);
    return () => {
      document.removeEventListener("pointerdown", onDown, true);
      if (autoFocus && restore.current && document.contains(restore.current)) restore.current.focus({ preventScroll: true });
    };
  }, [open, anchor, autoFocus]);

  if (!open || !anchor || typeof document === "undefined") return null;
  return createPortal(
    <div
      ref={ref}
      data-floating
      role={role}
      aria-label={label}
      style={{ position: "fixed", top: pos?.top ?? -9999, left: pos?.left ?? -9999 }}
      className={cn("z-(--app-z-popover) rounded-lg border border-border bg-surface-raised p-1 shadow-lg animate-pop-in", className)}
      onKeyDown={(e: KeyboardEvent<HTMLDivElement>) => {
        if (e.key === "Escape") {
          e.stopPropagation();
          closeRef.current();
        }
      }}
    >
      {children}
    </div>,
    document.body,
  );
}

export const floatingItemClass =
  "flex min-h-control w-full cursor-pointer select-none items-center gap-3 rounded-sm px-3 text-left type-body text-fg outline-none hover:bg-sunken focus-visible:bg-sunken aria-selected:bg-sunken disabled:cursor-default disabled:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0 pointer-coarse:min-h-tap";

export interface FloatingMenuItem {
  id: string;
  label: string;
  icon?: ReactNode;
  hint?: string;
  destructive?: boolean;
  disabled?: boolean;
  onSelect: () => void;
}

/** A keyboard-navigable list of actions for a Floating panel. */
export function FloatingMenu({ items, onDone }: { items: (FloatingMenuItem | "separator")[]; onDone: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const move = (dir: 1 | -1) => {
    const list = Array.from(ref.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? []);
    const i = list.indexOf(document.activeElement as HTMLButtonElement);
    list[(i + dir + list.length) % list.length]?.focus();
  };
  return (
    <div
      ref={ref}
      role="menu"
      className="flex min-w-56 max-w-80 flex-col"
      onKeyDown={(e) => {
        if (e.key === "ArrowDown") {
          e.preventDefault();
          move(1);
        } else if (e.key === "ArrowUp") {
          e.preventDefault();
          move(-1);
        }
      }}
    >
      {items.map((it, i) =>
        it === "separator" ? (
          <div key={`sep-${i}`} role="separator" className="my-1 h-px bg-border" />
        ) : (
          <button
            key={it.id}
            type="button"
            role="menuitem"
            disabled={it.disabled}
            className={cn(floatingItemClass, it.destructive && "text-danger-strong")}
            onClick={() => {
              onDone();
              it.onSelect();
            }}
          >
            {it.icon}
            <span className="min-w-0 flex-1 truncate">{it.label}</span>
            {it.hint ? <span className="shrink-0 type-caption text-fg-subtle">{it.hint}</span> : null}
          </button>
        ),
      )}
    </div>
  );
}
