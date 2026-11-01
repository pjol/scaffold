"use client";

import { pushRecentSearch } from "@scaffoldapp/shared";
import { useCallback, useEffect, useState } from "react";

const PREFIX = "scaffoldapp.search.recent.";

function read(key: string): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(key) ?? "[]");
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string").slice(0, 8) : [];
  } catch {
    return [];
  }
}

/**
 * Recent searches, kept only in this browser and keyed by user so a shared computer never
 * shows one account's searches to another.
 */
export function useRecentSearches(userId: string | undefined) {
  const key = userId ? PREFIX + userId : null;
  const [list, setList] = useState<string[]>([]);
  useEffect(() => setList(key ? read(key) : []), [key]);
  const write = useCallback(
    (next: string[]) => {
      setList(next);
      if (!key) return;
      try {
        localStorage.setItem(key, JSON.stringify(next));
      } catch {
        // Storage full or blocked: recent searches are a convenience.
      }
    },
    [key],
  );
  return {
    recent: list,
    remember: (q: string) => write(pushRecentSearch(key ? read(key) : list, q)),
    forget: (q: string) => write(list.filter((x) => x !== q)),
    clear: () => write([]),
  };
}
