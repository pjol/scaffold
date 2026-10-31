import { useCallback, useRef, useState } from "react";

/** Controlled/uncontrolled value: `value` wins when defined, else internal state. */
export function useControllableState<T>(opts: { value: T | undefined; defaultValue: T; onChange?: ((v: T) => void) | undefined }): [T, (v: T) => void] {
  const [inner, setInner] = useState(opts.defaultValue);
  const controlled = opts.value !== undefined;
  const onChange = useRef(opts.onChange);
  onChange.current = opts.onChange;
  const set = useCallback(
    (v: T) => {
      if (!controlled) setInner(v);
      onChange.current?.(v);
    },
    [controlled],
  );
  return [controlled ? (opts.value as T) : inner, set];
}
