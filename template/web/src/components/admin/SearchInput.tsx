"use client";

import { useDebouncedValue } from "@scaffoldapp/shared";
import { IconButton, Input } from "@scaffoldapp/ui-web";
import { Search, X } from "lucide-react";
import { useEffect, useState } from "react";

/** Debounced search field: calls `onSearch` 250 ms after typing stops; Escape or × clears. */
export function SearchInput({ value, onSearch, placeholder, label, className }: { value: string; onSearch: (q: string) => void; placeholder: string; label: string; className?: string }) {
  const [text, setText] = useState(value);
  const debounced = useDebouncedValue(text, 250);
  useEffect(() => setText(value), [value]);
  useEffect(() => {
    if (debounced.trim() !== value.trim()) onSearch(debounced.trim());
    // Only the debounced text triggers a search.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debounced]);
  return (
    <Input
      type="search"
      value={text}
      onChange={(e) => setText(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === "Escape" && text) {
          e.stopPropagation();
          setText("");
          onSearch("");
        }
      }}
      placeholder={placeholder}
      aria-label={label}
      leadingIcon={<Search />}
      wrapperClassName={className}
      className="[&::-webkit-search-cancel-button]:hidden"
      trailing={
        text ? (
          <IconButton
            label="Clear search"
            icon={<X />}
            size="sm"
            onClick={() => {
              setText("");
              onSearch("");
            }}
          />
        ) : undefined
      }
    />
  );
}
