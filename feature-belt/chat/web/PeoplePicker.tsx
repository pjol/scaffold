"use client";

import { useChatPeople, useDebouncedValue, type ChatUser } from "@scaffoldapp/shared";
import { Checkbox, EmptyState, ErrorState, Input, SkeletonRow, cn } from "@scaffoldapp/ui-web";
import { Search, UserRoundSearch } from "lucide-react";
import { useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";

export interface PeoplePickerProps {
  /** Single: picking someone calls `onPick`. Multiple: rows toggle `selected`. */
  multiple?: boolean;
  selected?: ChatUser[];
  onToggle?: (user: ChatUser) => void;
  onPick?: (user: ChatUser) => void;
  /** People already in the conversation (shown disabled). */
  exclude?: string[];
  disabled?: boolean;
}

/** Search people by name or exact email; recent contacts before typing. */
export function PeoplePicker({ multiple, selected = [], onToggle, onPick, exclude = [], disabled }: PeoplePickerProps) {
  const [query, setQuery] = useState("");
  const q = useDebouncedValue(query.trim(), 250);
  const people = useChatPeople(q.length === 1 ? "" : q);
  const selectedIds = new Set(selected.map((u) => u.id));
  const excluded = new Set(exclude);

  return (
    <div className="flex flex-col gap-3">
      <Input autoFocus value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Name or email" aria-label="Search people" leadingIcon={<Search />} />
      <div className="max-h-80 min-h-40 overflow-y-auto rounded-md border border-border">
        {people.isPending ? (
          <div className="flex flex-col gap-1 p-2" aria-hidden>
            {Array.from({ length: 4 }, (_, i) => (
              <SkeletonRow key={i} />
            ))}
          </div>
        ) : people.isError ? (
          <ErrorState compact title="Couldn't load people" info={people.error.message} onRetry={() => void people.refetch()} retrying={people.isFetching} />
        ) : people.data.items.length === 0 ? (
          <EmptyState compact icon={<UserRoundSearch />} title={q ? "No one found" : "Search for someone to message"} />
        ) : (
          <ul className="flex flex-col p-1" aria-label="People">
            {people.data.items.map((u) => {
              const already = excluded.has(u.id);
              const checked = selectedIds.has(u.id) || already;
              const row = (
                <>
                  <AppAvatar name={u.display_name} src={u.avatar_url} size="sm" />
                  <span className="min-w-0 flex-1 truncate type-body text-fg">{u.display_name}</span>
                  {already && <span className="type-caption text-fg-muted">In the group</span>}
                </>
              );
              return (
                <li key={u.id}>
                  {multiple ? (
                    <label className={cn("flex min-h-tap items-center gap-3 rounded-sm px-3 py-2 hover:bg-sunken", (already || disabled) && "opacity-60")}>
                      <Checkbox checked={checked} disabled={already || disabled} onCheckedChange={() => onToggle?.(u)} aria-label={u.display_name} />
                      {row}
                    </label>
                  ) : (
                    <button
                      type="button"
                      disabled={already || disabled}
                      onClick={() => onPick?.(u)}
                      className="flex min-h-tap w-full items-center gap-3 rounded-sm px-3 py-2 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-60"
                    >
                      {row}
                    </button>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
