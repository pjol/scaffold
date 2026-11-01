"use client";

import { reposShortSha, useReposRefs } from "@scaffoldapp/shared";
import { Badge, Button, Dialog, Input, Menu, MenuItem, MenuLabel, MenuSeparator, cn } from "@scaffoldapp/ui-web";
import { Check, ChevronDown, GitBranch, Search, Tag } from "lucide-react";
import { useMemo, useState } from "react";

const MENU_ROWS = 12;

export interface RefPickerProps {
  repoId: string;
  /** The ref shown (branch, tag or commit id). */
  value: string;
  onChange: (ref: string) => void;
  /** Branches only (comparisons, commit targets). */
  branchesOnly?: boolean;
  label?: string;
  className?: string;
}

/** Branch and tag switcher: a menu for the first dozen, a searchable list for the rest. */
export function RefPicker({ repoId, value, onChange, branchesOnly, label = "Switch branch or tag", className }: RefPickerProps) {
  const refs = useReposRefs(repoId);
  const [all, setAll] = useState(false);
  const items = useMemo(() => (refs.data?.items ?? []).filter((r) => !branchesOnly || r.kind === "branch"), [refs.data, branchesOnly]);
  const branches = items.filter((r) => r.kind === "branch");
  const tags = items.filter((r) => r.kind === "tag");
  const isSha = /^[0-9a-f]{40}$/.test(value);
  const shown = isSha ? reposShortSha(value) : value;
  const Icon = tags.some((t) => t.name === value) ? Tag : GitBranch;
  return (
    <>
      <Menu
        align="start"
        trigger={
          <Button variant="outline" size="md" leftIcon={<Icon />} rightIcon={<ChevronDown />} aria-label={`${label}: ${shown}`} className={cn("max-w-64", className)}>
            <span className="truncate font-mono">{shown}</span>
          </Button>
        }
      >
        <MenuLabel>Branches</MenuLabel>
        {branches.slice(0, MENU_ROWS).map((b) => (
          <MenuItem key={b.name} icon={b.name === value ? <Check /> : <GitBranch />} onSelect={() => onChange(b.name)}>
            <span className="flex min-w-0 items-center gap-2">
              <span className="truncate">{b.name}</span>
              {b.default ? <Badge tone="neutral">default</Badge> : null}
            </span>
          </MenuItem>
        ))}
        {!branchesOnly && tags.length > 0 ? (
          <>
            <MenuSeparator />
            <MenuLabel>Tags</MenuLabel>
            {tags.slice(0, 6).map((t) => (
              <MenuItem key={t.name} icon={t.name === value ? <Check /> : <Tag />} onSelect={() => onChange(t.name)}>
                {t.name}
              </MenuItem>
            ))}
          </>
        ) : null}
        {items.length > MENU_ROWS ? (
          <>
            <MenuSeparator />
            <MenuItem icon={<Search />} onSelect={() => setAll(true)}>
              Find a branch or tag
            </MenuItem>
          </>
        ) : null}
      </Menu>
      <Dialog open={all} onOpenChange={setAll} title={label}>
        <RefSearch
          names={items.map((r) => ({ name: r.name, kind: r.kind }))}
          value={value}
          onPick={(r) => {
            setAll(false);
            onChange(r);
          }}
        />
      </Dialog>
    </>
  );
}

function RefSearch({ names, value, onPick }: { names: { name: string; kind: "branch" | "tag" }[]; value: string; onPick: (ref: string) => void }) {
  const [q, setQ] = useState("");
  const hits = names.filter((n) => n.name.toLowerCase().includes(q.toLowerCase())).slice(0, 100);
  return (
    <div className="flex flex-col gap-3">
      <Input autoFocus aria-label="Filter" placeholder="Filter branches and tags" leadingIcon={<Search aria-hidden />} value={q} onChange={(e) => setQ(e.target.value)} />
      <ul className="flex max-h-80 flex-col overflow-y-auto">
        {hits.map((n) => (
          <li key={n.kind + n.name}>
            <button
              type="button"
              onClick={() => onPick(n.name)}
              className="flex min-h-control w-full items-center gap-2 rounded-sm px-2 text-left type-label hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
            >
              {n.kind === "tag" ? <Tag className="size-4 text-fg-muted" aria-hidden /> : <GitBranch className="size-4 text-fg-muted" aria-hidden />}
              <span className="min-w-0 flex-1 truncate">{n.name}</span>
              {n.name === value ? <Check className="size-4" aria-label="Current" /> : null}
            </button>
          </li>
        ))}
        {hits.length === 0 ? <li className="px-2 py-3 type-label text-fg-muted">Nothing matches.</li> : null}
      </ul>
    </div>
  );
}
