"use client";

import { useCallback, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";

/** `useLayoutEffect` in the browser, `useEffect` during SSR (no warning). */
export const useIsomorphicLayoutEffect = typeof window !== "undefined" ? useLayoutEffect : useEffect;

/** A media query's live value. `fallback` during SSR and hydration, then the real value. */
export function useMediaQuery(query: string, fallback = false): boolean {
  const subscribe = useCallback(
    (onChange: () => void) => {
      if (typeof window === "undefined" || !window.matchMedia) return () => {};
      const mql = window.matchMedia(query);
      mql.addEventListener("change", onChange);
      return () => mql.removeEventListener("change", onChange);
    },
    [query],
  );
  return useSyncExternalStore(
    subscribe,
    () => (typeof window !== "undefined" && window.matchMedia ? window.matchMedia(query).matches : fallback),
    () => fallback,
  );
}

/** True when the primary pointer can hover precisely (mouse, trackpad). False on touch. */
export function useHoverCapable(): boolean {
  return useMediaQuery("(hover: hover) and (pointer: fine)", true);
}

/** True at ≥ 1024px, where the shell shows its side navigation. */
export function useIsDesktop(query = "(min-width: 1024px)"): boolean {
  return useMediaQuery(query, false);
}

/** The latest value in a ref, for stable callbacks that read fresh props. */
export function useLatest<T>(value: T) {
  const ref = useRef(value);
  useIsomorphicLayoutEffect(() => {
    ref.current = value;
  });
  return ref;
}

/** Controlled / uncontrolled state in one hook. */
export function useControllableState<T>(opts: { value: T | undefined; defaultValue: T; onChange?: ((value: T) => void) | undefined }): [T, (next: T) => void] {
  const [inner, setInner] = useState<T>(opts.defaultValue);
  const controlled = opts.value !== undefined;
  const value = controlled ? (opts.value as T) : inner;
  const onChange = useLatest(opts.onChange);
  const set = useCallback(
    (next: T) => {
      if (!controlled) setInner(next);
      onChange.current?.(next);
    },
    [controlled, onChange],
  );
  return [value, set];
}

/** Whether the component has mounted in the browser. */
export function useMounted(): boolean {
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  return mounted;
}
