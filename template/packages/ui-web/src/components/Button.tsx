"use client";

import { AnimatePresence, motion, type HTMLMotionProps } from "motion/react";
import { Children, cloneElement, forwardRef, isValidElement, type ReactElement, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { Slot } from "../lib/slot";
import { useMotionTokens } from "../motion/tokens";
import { Spinner } from "./Spinner";

export type ButtonVariant = "primary" | "secondary" | "outline" | "ghost" | "soft" | "accent" | "danger" | "danger-ghost" | "success";
/** `sm` 32 (pointer only) · `md` 40 (web default) · `lg` 48 (mobile default) · `auto` 48 below 640 px, 40 above. */
export type ButtonSize = "sm" | "md" | "lg" | "auto";

const MotionSlot = motion.create(Slot);

// Class names stay literal so Tailwind's scanner sees them. Hover = the fill mixed 12% toward its text colour.
const variants: Record<ButtonVariant, string> = {
  primary: "bg-primary text-primary-fg hover:bg-[color-mix(in_oklab,var(--app-primary),var(--app-primary-fg)_12%)]",
  secondary: "bg-fg/8 text-fg hover:bg-fg/12",
  outline: "border border-border-strong bg-transparent text-fg hover:bg-sunken",
  ghost: "bg-transparent text-fg hover:bg-sunken",
  soft: "bg-primary-soft text-primary-strong hover:bg-[color-mix(in_oklab,var(--app-primary-soft),var(--app-primary)_12%)]",
  accent: "bg-accent text-accent-fg hover:bg-[color-mix(in_oklab,var(--app-accent),var(--app-accent-fg)_12%)]",
  danger: "bg-danger text-danger-fg hover:bg-[color-mix(in_oklab,var(--app-danger),var(--app-danger-fg)_12%)]",
  "danger-ghost": "bg-transparent text-danger-strong hover:bg-danger-soft",
  success: "bg-success text-success-fg hover:bg-[color-mix(in_oklab,var(--app-success),var(--app-success-fg)_12%)]",
};

// Heights and padding from design.md; icon↔label 8 (6 in sm). On coarse pointers the
// transparent ::before grows the hit area of sm/md to 44 px without changing the look.
const sizes: Record<ButtonSize, string> = {
  sm: "h-control-sm gap-1.5 rounded-sm px-3 text-label font-semibold [&_svg]:size-4 pointer-coarse:before:-inset-y-1.5",
  md: "h-control gap-2 rounded-md px-4 text-label font-semibold [&_svg]:size-4 pointer-coarse:before:-inset-y-0.5",
  lg: "h-control-lg gap-2 rounded-md px-5 text-body font-semibold [&_svg]:size-5",
  auto: "h-control-lg gap-2 rounded-md px-5 text-body font-semibold [&_svg]:size-5 sm:h-control sm:px-4 sm:text-label sm:[&_svg]:size-4 sm:pointer-coarse:before:-inset-y-0.5",
};

const spinnerSize: Record<ButtonSize, number> = { sm: 14, md: 16, lg: 18, auto: 16 };

export interface ButtonProps extends Omit<HTMLMotionProps<"button">, "children"> {
  /** Visual style. One `primary` per screen. @default "primary" */
  variant?: ButtonVariant;
  /** @default "md" */
  size?: ButtonSize;
  /** Spinner, disabled, `aria-busy`. */
  loading?: boolean;
  /** Icon before the label (a lucide element). Replaced by the spinner while loading. */
  leftIcon?: ReactNode;
  /** Icon after the label. */
  rightIcon?: ReactNode;
  /** Stretch to the container width. */
  fullWidth?: boolean;
  /** Style the child element (a Next `<Link>`, an `<a>`) as the button. */
  asChild?: boolean;
  children?: ReactNode;
}

/**
 * Button with spring press feedback (scale 0.97). Labels are verbs naming the outcome
 * ("Save", "Delete account").
 *
 * ```tsx
 * <Button loading={save.isPending} onClick={submit}>Save</Button>
 * <Button asChild variant="secondary"><Link href="/settings">Settings</Link></Button>
 * ```
 */
export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = "primary", size = "md", loading = false, leftIcon, rightIcon, fullWidth, asChild, disabled, className, children, type, ...rest },
  ref,
) {
  const t = useMotionTokens();
  const isDisabled = disabled || loading;
  const classes = cn(
    "relative inline-flex shrink-0 select-none items-center justify-center whitespace-nowrap",
    "before:absolute before:inset-x-0 before:inset-y-0 before:content-['']",
    "transition-[background-color,border-color,color,opacity] duration-(--app-duration-fast)",
    "disabled:cursor-not-allowed disabled:opacity-50 aria-disabled:cursor-not-allowed aria-disabled:opacity-50",
    variants[variant],
    sizes[size],
    fullWidth && "w-full",
    className,
  );
  const motionProps = { whileTap: isDisabled ? undefined : { scale: t.pressScale }, transition: t.spring.press };
  const lead = loading ? <Spinner size={spinnerSize[size]} /> : leftIcon;

  if (asChild) {
    const only = Children.only(children);
    const child =
      isValidElement<{ children?: ReactNode }>(only) && (lead || rightIcon)
        ? cloneElement(
            only as ReactElement<{ children?: ReactNode }>,
            undefined,
            <>
              {lead ? <span className="inline-flex">{lead}</span> : null}
              {only.props.children !== undefined && only.props.children !== null && <span className="truncate">{only.props.children}</span>}
              {rightIcon ? <span className="inline-flex">{rightIcon}</span> : null}
            </>,
          )
        : only;
    return (
      <MotionSlot ref={ref as never} className={classes} aria-disabled={isDisabled || undefined} aria-busy={loading || undefined} {...motionProps} {...(rest as object)}>
        {child}
      </MotionSlot>
    );
  }

  return (
    <motion.button ref={ref} type={type ?? "button"} className={classes} disabled={isDisabled} aria-busy={loading || undefined} {...motionProps} {...rest}>
      <AnimatePresence initial={false} mode="popLayout">
        {lead ? (
          <motion.span
            key={loading ? "spinner" : "icon"}
            className="inline-flex"
            initial={{ opacity: 0, scale: 0.6 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.6 }}
            transition={t.spring.snappy}
          >
            {lead}
          </motion.span>
        ) : null}
      </AnimatePresence>
      {children !== undefined && children !== null && <span className="truncate">{children}</span>}
      {rightIcon ? <span className="inline-flex">{rightIcon}</span> : null}
    </motion.button>
  );
});
