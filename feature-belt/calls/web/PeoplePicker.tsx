"use client";

import { orgsNounA, useCallsPeople, useDebouncedValue, type CallsUser } from "@scaffoldapp/shared";
import { Checkbox, EmptyState, ErrorState, IconButton, Input, SkeletonRow, cn } from "@scaffoldapp/ui-web";
import { Search, UserRoundSearch, X } from "lucide-react";
import { useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";

export interface CallsPeoplePickerProps {
  selected: CallsUser[];
  onChange: (people: CallsUser[]) => void;
  /** Limit the directory to one organization. */
  orgId?: string | null;
  /** People already in (shown checked and disabled). */
  exclude?: string[];
  max?: number;
  disabled?: boolean;
}

/** Search people you may call or invite, by name or exact email; chosen people show as chips. */
export function CallsPeoplePicker({ selected, onChange, orgId, exclude = [], max = 16, disabled }: CallsPeoplePickerProps) {
  const [query, setQuery] = useState("");
  const q = useDebouncedValue(query.trim(), 250);
  const people = useCallsPeople(q.length === 1 ? "" : q, orgId);
  const chosen = new Set(selected.map((u) => u.id));
  const excluded = new Set(exclude);
  const full = selected.length >= max;

  const toggle = (u: CallsUser) => onChange(chosen.has(u.id) ? selected.filter((x) => x.id !== u.id) : [...selected, u]);

  return (
    <div className="flex flex-col gap-3">
      {selected.length > 0 && (
        <ul className="flex flex-wrap gap-2" aria-label="Chosen people">
          {selected.map((u) => (
            <li key={u.id} className="flex h-control-sm items-center gap-1.5 rounded-pill bg-primary-soft pr-1 pl-1 text-primary-strong">
              <AppAvatar name={u.display_name} src={u.avatar_url} size="xs" />
              <span className="max-w-40 truncate type-label">{u.display_name}</span>
              <IconButton label={`Remove ${u.display_name}`} icon={<X />} variant="ghost" size="sm" shape="circle" onClick={() => toggle(u)} disabled={disabled} />
            </li>
          ))}
        </ul>
      )}
      <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Name or email" aria-label="Search people" leadingIcon={<Search />} disabled={disabled} />
      <div className="max-h-72 min-h-40 overflow-y-auto rounded-md border border-border">
        {people.isPending ? (
          <div className="flex flex-col gap-1 p-2" aria-hidden>
            {Array.from({ length: 4 }, (_, i) => (
              <SkeletonRow key={i} />
            ))}
          </div>
        ) : people.isError ? (
          <ErrorState compact title="Couldn't load people" info={people.error.message} onRetry={() => void people.refetch()} retrying={people.isFetching} />
        ) : people.data.items.length === 0 ? (
          <EmptyState compact icon={<UserRoundSearch />} title={q ? "No one found" : "No one to add yet"} info={`You can add people who share ${orgsNounA()} with you.`} />
        ) : (
          <ul className="flex flex-col p-1" aria-label="People">
            {people.data.items.map((u) => {
              const already = excluded.has(u.id);
              const checked = chosen.has(u.id) || already;
              const off = already || disabled || (full && !checked);
              return (
                <li key={u.id}>
                  <label className={cn("flex min-h-tap items-center gap-3 rounded-sm px-3 py-2 hover:bg-sunken", off && "opacity-60")}>
                    <Checkbox checked={checked} disabled={off} onCheckedChange={() => toggle(u)} aria-label={u.display_name} />
                    <AppAvatar name={u.display_name} src={u.avatar_url} size="sm" />
                    <span className="min-w-0 flex-1 truncate type-body text-fg">{u.display_name}</span>
                    {already && <span className="type-caption text-fg-muted">Already in</span>}
                  </label>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
