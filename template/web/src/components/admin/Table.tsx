"use client";

import { InfoTip, Skeleton, SkeletonGroup, cn } from "@scaffoldapp/ui-web";
import type { KeyboardEvent, MouseEvent, ReactNode } from "react";

/** Data table that scrolls sideways inside its card when the screen is narrower than its min width (`className="min-w-160"`). */
export function Table({ children, className, label }: { children: ReactNode; className?: string; label: string }) {
  return (
    <div className="w-full overflow-x-auto">
      <table aria-label={label} className={cn("w-full border-separate border-spacing-0 text-left type-label", className)}>
        {children}
      </table>
    </div>
  );
}

export function THead({ children }: { children: ReactNode }) {
  return (
    <thead>
      <tr>{children}</tr>
    </thead>
  );
}

export function TH({ children, info, align = "left", className }: { children?: ReactNode; info?: ReactNode; align?: "left" | "right"; className?: string }) {
  return (
    <th scope="col" className={cn("relative h-row border-b border-border px-3 type-caption font-semibold whitespace-nowrap text-fg-muted first:pl-4 last:pr-4", align === "right" && "text-right", className)}>
      <span className={cn("inline-flex items-center gap-1", align === "right" && "flex-row-reverse")}>
        {children}
        {info && <InfoTip content={info} />}
      </span>
    </th>
  );
}

/** A row; with `onActivate` the whole row opens its target (links and buttons inside keep their own clicks). */
export function TR({ children, onActivate, className }: { children: ReactNode; onActivate?: () => void; className?: string }) {
  const onClick = (e: MouseEvent<HTMLTableRowElement>) => {
    if ((e.target as HTMLElement).closest("a,button,input,label,summary,select")) return;
    onActivate?.();
  };
  const onKeyDown = (e: KeyboardEvent<HTMLTableRowElement>) => {
    if (e.target !== e.currentTarget) return;
    if (e.key === "Enter") {
      e.preventDefault();
      onActivate?.();
    }
  };
  return (
    <tr
      onClick={onActivate ? onClick : undefined}
      onKeyDown={onActivate ? onKeyDown : undefined}
      className={cn("transition-colors [&>td]:border-b [&>td]:border-border last:[&>td]:border-b-0", onActivate && "cursor-pointer hover:bg-sunken", className)}
    >
      {children}
    </tr>
  );
}

export function TD({ children, align = "left", className }: { children?: ReactNode; align?: "left" | "right"; className?: string }) {
  return <td className={cn("relative h-row px-3 py-2 align-middle text-fg first:pl-4 last:pr-4", align === "right" && "text-right tabular-nums", className)}>{children}</td>;
}

/** Table-shaped skeleton. */
export function TableSkeleton({ rows = 6, cols = 4 }: { rows?: number; cols?: number }) {
  return (
    <SkeletonGroup className="flex flex-col">
      {Array.from({ length: rows }, (_, r) => (
        <div key={r} className="flex h-row items-center gap-4 border-b border-border px-4 last:border-b-0">
          {Array.from({ length: cols }, (_, c) => (
            <Skeleton key={c} shape="text" className={cn("h-4 flex-1", c === 0 && "max-w-40")} />
          ))}
        </div>
      ))}
    </SkeletonGroup>
  );
}
