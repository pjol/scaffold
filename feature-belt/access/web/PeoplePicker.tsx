"use client";

import { useAccessDirectory, type AccessDirectoryEntry } from "@scaffoldapp/shared";
import { Input, Spinner, cn } from "@scaffoldapp/ui-web";
import { Search, Users } from "lucide-react";
import { useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { memberCount } from "./labels";

/** A person's or team's avatar; teams fall back to a people icon. */
export function SubjectAvatar({ type, name, src, size = "sm" }: { type: string; name: string; src: string | null; size?: "xs" | "sm" | "md" | "lg" }) {
  return <AppAvatar name={name} src={src} size={size} {...(type === "team" ? { fallback: <Users className="size-1/2" aria-hidden /> } : {})} />;
}

export interface PeoplePickerProps {
  /** The org whose members and teams are offered. */
  orgId: string;
  /** Default both. */
  types?: "user" | "team" | "user,team";
  /** Ids not to offer (already chosen, or already have access). */
  exclude?: ReadonlySet<string>;
  onPick: (entry: AccessDirectoryEntry) => void;
  /** Accessible name. @default "Add people or teams" */
  label?: string;
  placeholder?: string;
  autoFocus?: boolean;
}

/**
 * Search the org's people and teams and pick one (combobox: arrow keys, Enter, Escape).
 * Results open below the field in the page flow, so it works inside dialogs and sheets.
 */
export function PeoplePicker({ orgId, types = "user,team", exclude, onPick, label = "Add people or teams", placeholder, autoFocus }: PeoplePickerProps) {
  const listId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const dir = useAccessDirectory(orgId, query, { types, limit: 8, enabled: open });
  const items = useMemo(() => (dir.data ?? []).filter((e) => !exclude?.has(e.id)), [dir.data, exclude]);
  const showList = open && (query.trim().length > 0 || items.length > 0);

  const pick = (e: AccessDirectoryEntry) => {
    onPick(e);
    setQuery("");
    setActive(0);
    inputRef.current?.focus();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setOpen(true);
      setActive((i) => Math.min(i + 1, Math.max(items.length - 1, 0)));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter" && showList && items[active]) {
      e.preventDefault();
      pick(items[active]);
    } else if (e.key === "Escape" && showList) {
      e.preventDefault();
      e.stopPropagation();
      setOpen(false);
    }
  };

  const typeHint = types === "team" ? "teams" : types === "user" ? "people" : "people or teams";
  return (
    <div className="flex flex-col gap-1">
      <Input
        ref={inputRef}
        role="combobox"
        aria-label={label}
        aria-expanded={showList}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={showList && items[active] ? `${listId}-${items[active].id}` : undefined}
        autoComplete="off"
        spellCheck={false}
        value={query}
        placeholder={placeholder ?? `Search ${typeHint}`}
        leadingIcon={<Search aria-hidden />}
        trailing={dir.isFetching && open ? <Spinner size={16} label="Searching" /> : undefined}
        onChange={(e) => {
          setQuery(e.target.value);
          setOpen(true);
          setActive(0);
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setTimeout(() => setOpen(false), 150)}
        onKeyDown={onKeyDown}
        {...(autoFocus ? { autoFocus: true } : {})}
      />
      {showList ? (
        <ul id={listId} role="listbox" aria-label={label} className="flex max-h-64 flex-col overflow-y-auto rounded-md border border-border bg-surface p-1">
          {dir.isError ? (
            <li className="px-3 py-2 type-label text-danger-strong">Couldn&apos;t search. Check your connection.</li>
          ) : items.length === 0 ? (
            <li className="px-3 py-2 type-label text-fg-muted">{dir.isFetching ? "Searching…" : `No ${typeHint} match “${query.trim()}”`}</li>
          ) : (
            items.map((e, i) => (
              <li
                key={e.id}
                id={`${listId}-${e.id}`}
                role="option"
                aria-selected={i === active}
                className={cn("flex min-h-control cursor-pointer items-center gap-3 rounded-sm px-2 py-1.5 pointer-coarse:min-h-tap", i === active && "bg-sunken")}
                onMouseEnter={() => setActive(i)}
                onMouseDown={(ev) => ev.preventDefault()}
                onClick={() => pick(e)}
              >
                <SubjectAvatar type={e.type} name={e.name} src={e.avatar_url} />
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate type-label text-fg">{e.name}</span>
                </span>
                <span className="shrink-0 type-caption text-fg-muted">
                  {e.type === "team" ? `@${e.handle} · ${memberCount(e.member_count)}` : (e.email ?? "")}
                </span>
              </li>
            ))
          )}
        </ul>
      ) : null}
    </div>
  );
}
