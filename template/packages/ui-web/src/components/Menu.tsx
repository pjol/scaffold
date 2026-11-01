"use client";

import * as MenuPrimitive from "@radix-ui/react-dropdown-menu";
import { Children, cloneElement, isValidElement, type ReactElement, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useMediaQuery } from "../lib/hooks";

export interface MenuProps {
  /** One focusable element that opens the menu (rendered with `asChild`). */
  trigger: ReactNode;
  children: ReactNode;
  align?: "start" | "center" | "end";
  side?: "top" | "right" | "bottom" | "left";
  className?: string;
}

/**
 * Dropdown menu (Radix): keyboard navigable, closes on select. Destructive items go
 * last, in `danger`, and open a confirmation.
 */
export function Menu({ trigger, children, align = "end", side = "bottom", className }: MenuProps) {
  return (
    <MenuPrimitive.Root modal={false}>
      <MenuPrimitive.Trigger asChild>{trigger}</MenuPrimitive.Trigger>
      <MenuPrimitive.Portal>
        <MenuPrimitive.Content
          align={align}
          side={side}
          sideOffset={8}
          collisionPadding={12}
          className={cn(
            "z-(--app-z-popover) min-w-56 max-w-80 origin-(--radix-dropdown-menu-content-transform-origin) rounded-lg border border-border bg-surface-raised p-1 shadow-lg",
            "animate-pop-in data-[state=closed]:animate-pop-out",
            className,
          )}
        >
          {children}
        </MenuPrimitive.Content>
      </MenuPrimitive.Portal>
    </MenuPrimitive.Root>
  );
}

const itemClasses =
  "flex min-h-control cursor-pointer select-none items-center gap-3 rounded-sm px-3 type-body outline-none data-[highlighted]:bg-sunken data-[disabled]:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0 pointer-coarse:min-h-tap";

export interface MenuItemProps {
  icon?: ReactNode;
  children: ReactNode;
  onSelect?: (event: Event) => void;
  /** Danger-coloured (destructive actions, last in the menu). */
  destructive?: boolean;
  disabled?: boolean;
  /** Render the child (a link) as the item; `icon` is placed inside it. */
  asChild?: boolean;
}

export function MenuItem({ icon, children, onSelect, destructive, disabled, asChild }: MenuItemProps) {
  const className = cn(itemClasses, destructive ? "text-danger-strong" : "text-fg");
  if (asChild) {
    const child = Children.only(children);
    const content =
      icon && isValidElement(child)
        ? cloneElement(
            child as ReactElement<{ children?: ReactNode }>,
            undefined,
            icon,
            <span className="min-w-0 flex-1 truncate">{(child as ReactElement<{ children?: ReactNode }>).props.children}</span>,
          )
        : children;
    return (
      <MenuPrimitive.Item asChild className={className} disabled={disabled} {...(onSelect ? { onSelect } : {})}>
        {content}
      </MenuPrimitive.Item>
    );
  }
  return (
    <MenuPrimitive.Item className={className} disabled={disabled} {...(onSelect ? { onSelect } : {})}>
      {icon}
      <span className="min-w-0 flex-1 truncate">{children}</span>
    </MenuPrimitive.Item>
  );
}

export function MenuSeparator() {
  return <MenuPrimitive.Separator className="my-1 h-px bg-border" />;
}

/** Non-interactive header row (the signed-in name). */
export function MenuLabel({ children, className }: { children: ReactNode; className?: string }) {
  return <MenuPrimitive.Label className={cn("flex items-center gap-3 px-3 py-2 type-label text-fg-muted", className)}>{children}</MenuPrimitive.Label>;
}

export interface MenuSubProps {
  icon?: ReactNode;
  /** Row label; `hint` is the current value at the end of the row ("Read"). */
  label: ReactNode;
  hint?: ReactNode;
  disabled?: boolean;
  children: ReactNode;
}

/**
 * A row that opens a nested menu (a choice of levels). Fill it with MenuChoice rows.
 * Below 640 px there is no room beside the menu, so the choices show inline under a
 * heading row instead.
 */
export function MenuSub({ icon, label, hint, disabled, children }: MenuSubProps) {
  const narrow = useMediaQuery("(max-width: 639px)");
  if (narrow) {
    return (
      <MenuPrimitive.Group className={cn(disabled && "opacity-50")}>
        <MenuPrimitive.Label className="flex items-center gap-3 px-3 pt-2 pb-1 type-label text-fg-muted [&_svg]:size-4">
          {icon}
          <span className="min-w-0 flex-1 truncate">{label}</span>
          {hint ? <span className="shrink-0 type-caption">{hint}</span> : null}
        </MenuPrimitive.Label>
        {disabled ? null : children}
      </MenuPrimitive.Group>
    );
  }
  return (
    <MenuPrimitive.Sub>
      <MenuPrimitive.SubTrigger className={cn(itemClasses, "text-fg data-[state=open]:bg-sunken")} disabled={disabled}>
        {icon}
        <span className="min-w-0 flex-1 truncate">{label}</span>
        {hint ? <span className="shrink-0 type-caption text-fg-muted">{hint}</span> : null}
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} aria-hidden className="text-fg-subtle">
          <path d="m9 18 6-6-6-6" />
        </svg>
      </MenuPrimitive.SubTrigger>
      <MenuPrimitive.Portal>
        <MenuPrimitive.SubContent
          sideOffset={4}
          collisionPadding={12}
          className="z-(--app-z-popover) min-w-48 max-w-80 rounded-lg border border-border bg-surface-raised p-1 shadow-lg animate-pop-in data-[state=closed]:animate-pop-out"
        >
          {children}
        </MenuPrimitive.SubContent>
      </MenuPrimitive.Portal>
    </MenuPrimitive.Sub>
  );
}

export interface MenuChoiceProps {
  checked?: boolean;
  children: ReactNode;
  hint?: ReactNode;
  disabled?: boolean;
  onSelect: (event: Event) => void;
}

/** One option in a MenuSub: a check marks the current value. */
export function MenuChoice({ checked, children, hint, disabled, onSelect }: MenuChoiceProps) {
  return (
    <MenuPrimitive.Item className={cn(itemClasses, "text-fg")} disabled={disabled} onSelect={onSelect}>
      <span className="flex size-4 items-center justify-center" aria-hidden>
        {checked ? (
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.5}>
            <path d="M20 6 9 17l-5-5" />
          </svg>
        ) : null}
      </span>
      <span className="min-w-0 flex-1 truncate">{children}</span>
      {hint ? <span className="shrink-0 type-caption text-fg-muted">{hint}</span> : null}
      {checked ? <span className="sr-only">(current)</span> : null}
    </MenuPrimitive.Item>
  );
}
