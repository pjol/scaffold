"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback } from "react";

/**
 * A query-string param as state (`?status=suspended`), so filters survive refresh, back
 * and share. Setting the fallback removes the param; `replace` keeps history clean.
 */
export function useUrlState<V extends string = string>(key: string, fallback: NoInfer<V>): [V, (next: V | null) => void] {
  const params = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const value = (params.get(key) as V | null) ?? fallback;
  const set = useCallback(
    (next: V | null) => {
      const sp = new URLSearchParams(window.location.search);
      if (next === null || next === fallback || next === "") sp.delete(key);
      else sp.set(key, next);
      const qs = sp.toString();
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
    },
    [key, fallback, pathname, router],
  );
  return [value, set];
}
