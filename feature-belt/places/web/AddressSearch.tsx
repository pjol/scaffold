"use client";

import { errorMessage, useDebouncedValue, useGeocode, type GeoResult, type LatLng } from "@scaffoldapp/shared";
import { Input, Spinner, cn } from "@scaffoldapp/ui-web";
import { MapPin, Search } from "lucide-react";
import { useId, useState, type KeyboardEvent } from "react";

/**
 * Address search (server geocoding: Photon or Mapbox). Typing searches after 300 ms;
 * arrow keys and Enter pick a result.
 */
export function AddressSearch({ near, onPick, invalid, id }: { near?: LatLng | null; onPick: (r: GeoResult) => void; invalid?: boolean; id?: string }) {
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const q = useDebouncedValue(text, 300);
  const results = useGeocode(q, near);
  const items = q.trim().length >= 2 ? (results.data ?? []) : [];
  const listId = useId();

  const pick = (r: GeoResult) => {
    onPick(r);
    setText("");
    setOpen(false);
  };
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (!open || items.length === 0) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((a) => (a + 1) % items.length);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((a) => (a - 1 + items.length) % items.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      const r = items[active];
      if (r) pick(r);
    } else if (e.key === "Escape") {
      setOpen(false);
    }
  };

  return (
    <div className="flex flex-col gap-2">
      <Input
        id={id}
        role="combobox"
        aria-expanded={open && items.length > 0}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={open && items[active] ? `${listId}-${active}` : undefined}
        aria-invalid={invalid || undefined}
        value={text}
        placeholder="Search an address or place"
        leadingIcon={<Search />}
        trailing={results.isFetching ? <Spinner size={16} label="Searching" /> : undefined}
        onChange={(e) => {
          setText(e.target.value);
          setOpen(true);
          setActive(0);
        }}
        onFocus={() => setOpen(true)}
        onKeyDown={onKey}
        autoComplete="off"
      />
      {open && q.trim().length >= 2 ? (
        results.isError ? (
          <p role="alert" className="type-caption text-danger-strong">
            {errorMessage(results.error, "Address search isn't available right now. Drop a pin on the map instead.")}
          </p>
        ) : items.length > 0 ? (
          <ul id={listId} role="listbox" aria-label="Addresses" className="flex flex-col overflow-hidden rounded-md border border-border bg-surface">
            {items.map((r, i) => (
              <li
                key={`${r.lat},${r.lng},${r.label}`}
                id={`${listId}-${i}`}
                role="option"
                aria-selected={i === active}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => pick(r)}
                onMouseEnter={() => setActive(i)}
                className={cn("flex min-h-tap cursor-pointer items-center gap-3 px-3 py-2 type-label text-fg", i === active && "bg-sunken")}
              >
                <MapPin className="size-4 shrink-0 text-fg-muted" aria-hidden />
                <span className="min-w-0 flex-1 truncate">{r.label}</span>
              </li>
            ))}
          </ul>
        ) : !results.isFetching ? (
          <p className="type-caption text-fg-muted">No matches. Try a street and city, or drop a pin on the map.</p>
        ) : null
      ) : null}
    </div>
  );
}
