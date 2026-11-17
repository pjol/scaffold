"use client";

import { ChevronRight } from "lucide-react";
import { motion, type HTMLMotionProps } from "motion/react";
import { cloneElement, createContext, forwardRef, isValidElement, useContext, type KeyboardEvent, type ReactElement, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { resolveChild, Slot } from "../lib/slot";
import { useMotionTokens } from "../motion/tokens";
import { InfoTip } from "./InfoTip";

const MotionSlot = motion.create(Slot);

const InListGroup = createContext(false);

export interface ListRowProps extends Omit<HTMLMotionProps<"div">, "title"> {
  /** Row title. There is no subtitle prop: context goes in `info`. */
  title: ReactNode;
  /** Icon or avatar. Rows with one are 56 px tall, otherwise 48. */
  leading?: ReactNode;
  /** Value, badge, switch. */
  trailing?: ReactNode;
  /** ⓘ content next to the title. Static rows only: an interactive row can't contain another control. */
  info?: ReactNode;
  /** Show a chevron (on by default for interactive rows). */
  chevron?: boolean;
  /** Pressable (with `onClick`, or `asChild` + a link). */
  interactive?: boolean;
  /** Danger-coloured title (destructive rows go last). */
  destructive?: boolean;
  /** Mark as the current item (`aria-current`). */
  current?: boolean;
  asChild?: boolean;
}

/** One-line row: leading · title (+ⓘ) · trailing · chevron. Inset 16 on its own; in a `ListGroup` it spans the column. */
export const ListRow = forwardRef<HTMLDivElement, ListRowProps>(function ListRow(
  { title, leading, trailing, info, chevron, interactive, destructive, current, asChild, className, children, ...rest },
  ref,
) {
  const t = useMotionTokens();
  const isInteractive = interactive ?? Boolean(rest.onClick || asChild);
  const flush = useContext(InListGroup);
  const classes = cn(
    "flex w-full items-center gap-3 text-left",
    leading ? "min-h-tap-lg py-2" : "min-h-row py-2",
    // In a group the text stays on the column; hover and current paint past it, into the gutter. No radius on the row itself: the group's hairline runs along its edge.
    flush
      ? "relative z-0 before:absolute before:inset-y-0 before:-inset-x-2 before:-z-10 before:rounded-md before:transition-colors before:duration-(--app-duration-fast) before:content-['']"
      : "rounded-md px-4",
    isInteractive && "cursor-pointer transition-colors duration-(--app-duration-fast)",
    isInteractive && (flush ? "hover:before:bg-sunken" : "hover:bg-sunken"),
    current && (flush ? "before:bg-sunken" : "bg-sunken"),
    className,
  );
  const inner = (
    <>
      {leading && <span className="flex shrink-0 items-center">{leading}</span>}
      <span className="flex min-w-0 flex-1 items-center gap-2">
        <span className={cn("truncate type-body font-medium", destructive ? "text-danger-strong" : "text-fg")}>{title}</span>
        {info && !isInteractive && <InfoTip content={info} />}
      </span>
      {trailing && <span className="flex shrink-0 items-center gap-2 text-fg-muted">{trailing}</span>}
      {(chevron ?? isInteractive) && <ChevronRight className="size-4 shrink-0 text-fg-subtle" strokeWidth={2.25} aria-hidden />}
    </>
  );
  const press = isInteractive ? { whileTap: { scale: 0.99 }, transition: t.spring.press } : {};
  if (asChild) {
    const only = resolveChild(children as ReactNode);
    const child = isValidElement(only) ? cloneElement(only as ReactElement<{ children?: ReactNode }>, undefined, inner) : only;
    return (
      <MotionSlot ref={ref as never} className={classes} aria-current={current ? "page" : undefined} {...press} {...(rest as object)}>
        {child}
      </MotionSlot>
    );
  }
  return (
    <motion.div
      ref={ref}
      className={classes}
      {...(rest.onClick
        ? {
            role: "button",
            tabIndex: 0,
            onKeyDown: (e: KeyboardEvent<HTMLDivElement>) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                e.currentTarget.click();
              }
            },
          }
        : {})}
      {...press}
      {...rest}
    >
      {inner}
    </motion.div>
  );
});

/** Hairline divider. */
export function Divider({ className }: { className?: string }) {
  return <hr className={cn("border-0 border-t border-border", className)} />;
}

export interface ListGroupProps {
  /** Group heading (sentence case). */
  title?: ReactNode;
  info?: ReactNode;
  /** Rows, directly or in one `<ul>`/`Stagger as="ul"`; hairlines are drawn between them. */
  children: ReactNode;
  className?: string;
}

/** Rows on the canvas: optional heading, then rows that span the column with hairlines between. No box of its own, so the column doesn't shrink (design.md, house rule 8). */
export function ListGroup({ title, info, children, className }: ListGroupProps) {
  return (
    <div className={cn("flex flex-col gap-2", className)}>
      {title && (
        <h2 className="flex items-center gap-2 type-label font-semibold text-fg-muted">
          {title}
          {info && <InfoTip content={info} />}
        </h2>
      )}
      <InListGroup.Provider value={true}>
        <div className="flex flex-col divide-y divide-border [&>ul]:divide-y [&>ul]:divide-border">{children}</div>
      </InListGroup.Provider>
    </div>
  );
}
