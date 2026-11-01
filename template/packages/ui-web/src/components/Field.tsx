"use client";

import { CircleAlert } from "lucide-react";
import { createContext, useContext, useId, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { Collapse } from "../motion/Presence";
import { InfoTip } from "./InfoTip";

interface FieldContextValue {
  id: string;
  errorId: string;
  invalid: boolean;
  required: boolean;
  disabled: boolean;
}

const FieldContext = createContext<FieldContextValue | null>(null);

/** The surrounding {@link Field} (id, invalid…). Controls read it automatically. */
export function useField(): FieldContextValue | null {
  return useContext(FieldContext);
}

/** Props a control spreads to wire itself to its Field: `id`, `aria-invalid`, `aria-describedby`, `required`, `disabled`. Explicit props win. */
export function useFieldControlProps<P extends { id?: string | undefined; disabled?: boolean | undefined; required?: boolean | undefined }>(
  props: P,
): P & { "aria-invalid"?: boolean; "aria-describedby"?: string } {
  const f = useField();
  if (!f) return props;
  return {
    ...props,
    id: props.id ?? f.id,
    disabled: props.disabled ?? f.disabled,
    required: props.required ?? f.required,
    ...(f.invalid ? { "aria-invalid": true, "aria-describedby": f.errorId } : {}),
  };
}

export interface FieldProps {
  /** Short label. There is no helper-text slot: guidance goes in `info`. */
  label: ReactNode;
  /** ⓘ content next to the label. */
  info?: ReactNode;
  /** Validation message; animates in under the control. */
  error?: ReactNode;
  required?: boolean;
  disabled?: boolean;
  /** Node at the far end of the label row (a counter, a link). */
  labelEnd?: ReactNode;
  /** Explicit control id (otherwise generated). */
  id?: string;
  className?: string;
  children: ReactNode;
}

/**
 * Label (+ⓘ) · control · error, with label→control 8 px and control→error 4 px.
 *
 * ```tsx
 * <Field label="Name" error={errors.name}><Input value={name} onChange={…} /></Field>
 * ```
 */
export function Field({ label, info, error, required = false, disabled = false, labelEnd, id, className, children }: FieldProps) {
  const auto = useId();
  const controlId = id ?? `f${auto.replace(/[^a-zA-Z0-9]/g, "")}`;
  const errorId = `${controlId}-error`;
  const invalid = Boolean(error);
  return (
    <FieldContext.Provider value={{ id: controlId, errorId, invalid, required, disabled }}>
      <div className={cn("flex flex-col gap-2", disabled && "opacity-60", className)}>
        <div className="flex min-h-6 items-center gap-2">
          <label htmlFor={controlId} className="type-label text-fg">
            {label}
            {required && (
              <span aria-hidden className="ml-0.5 text-danger-strong">
                *
              </span>
            )}
          </label>
          {info && <InfoTip content={info} label={typeof label === "string" ? `About ${label}` : "More info"} />}
          {labelEnd && <div className="ml-auto flex items-center">{labelEnd}</div>}
        </div>
        <div className="flex flex-col gap-1">
          {children}
          <Collapse open={invalid}>
            <p id={errorId} role="alert" className="flex items-center gap-1.5 type-label text-danger-strong">
              <CircleAlert className="size-4 shrink-0" strokeWidth={2.25} aria-hidden />
              {error}
            </p>
          </Collapse>
        </div>
      </div>
    </FieldContext.Provider>
  );
}
