import { Children, cloneElement, forwardRef, isValidElement, type HTMLAttributes, type ReactElement, type ReactNode, type Ref } from "react";
import { cn } from "./cn";

type AnyProps = Record<string, unknown>;

/** Compose several refs into one callback ref. */
export function composeRefs<T>(...refs: (Ref<T> | undefined)[]) {
  return (node: T | null) => {
    for (const ref of refs) {
      if (typeof ref === "function") ref(node);
      else if (ref && typeof ref === "object") (ref as { current: T | null }).current = node;
    }
  };
}

function mergeProps(slot: AnyProps, child: AnyProps): AnyProps {
  const out: AnyProps = { ...slot, ...child };
  for (const key of Object.keys(slot)) {
    const s = slot[key];
    const c = child[key];
    if (/^on[A-Z]/.test(key) && typeof s === "function" && typeof c === "function") {
      out[key] = (...args: unknown[]) => {
        (c as (...a: unknown[]) => unknown)(...args);
        const ev = args[0] as { defaultPrevented?: boolean } | undefined;
        if (!ev?.defaultPrevented) (s as (...a: unknown[]) => unknown)(...args);
      };
    } else if (key === "style") {
      out["style"] = { ...(s as object), ...(c as object) };
    } else if (key === "className") {
      out["className"] = cn(s as string, c as string);
    }
  }
  return out;
}

export interface SlotProps extends HTMLAttributes<HTMLElement> {
  children?: ReactNode;
}

/**
 * Renders its single child with the slot's props, handlers, className and ref merged
 * in. Powers `asChild`: `<Button asChild><Link href="/x">Go</Link></Button>`.
 */
export const Slot = forwardRef<HTMLElement, SlotProps>(function Slot({ children, ...props }, ref) {
  const only = Children.only(children);
  if (!isValidElement(only)) return null;
  const child = only as ReactElement<AnyProps & { ref?: Ref<HTMLElement> }>;
  return cloneElement(child, {
    ...mergeProps(props as AnyProps, child.props),
    ref: composeRefs(ref, child.props.ref),
  });
});
