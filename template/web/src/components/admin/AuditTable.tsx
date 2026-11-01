"use client";

import type { AuditEntry } from "@scaffoldapp/shared";
import { IconButton } from "@scaffoldapp/ui-web";
import { Filter } from "lucide-react";
import Link from "next/link";
import { formatDateTime, formatRelative } from "@/lib/format";
import { Table, TD, TH, THead, TR } from "./Table";

function shortId(id: string) {
  return id.length > 12 ? `${id.slice(0, 8)}…` : id;
}

/** Where an audit target lives in the console, when it has a page. Features extend this for their target types. */
function targetHref(e: AuditEntry): string | null {
  if (e.target_type === "user") return `/admin/users/${e.target_id}`;
  return null;
}

function DataCell({ data }: { data: AuditEntry["data"] }) {
  if (!data || Object.keys(data).length === 0) return <span className="text-fg-subtle">None</span>;
  return (
    <details className="group max-w-md">
      <summary className="flex min-h-control cursor-pointer list-none items-center rounded-sm font-semibold text-primary-strong [&::-webkit-details-marker]:hidden">View</summary>
      <pre className="mt-1 overflow-x-auto rounded-md bg-sunken p-3 type-caption whitespace-pre-wrap text-fg">{JSON.stringify(data, null, 2)}</pre>
    </details>
  );
}

/** Audit entries, newest first. `onFilterTarget` adds a "only this target" button per row. */
export function AuditTable({ entries, onFilterTarget, showTarget = true }: { entries: AuditEntry[]; onFilterTarget?: (e: AuditEntry) => void; showTarget?: boolean }) {
  return (
    <Table label="Audit log" className="min-w-160">
      <THead>
        <TH>When</TH>
        <TH>Action</TH>
        {showTarget && <TH>Target</TH>}
        <TH>Staff</TH>
        <TH>Data</TH>
      </THead>
      <tbody>
        {entries.map((e) => {
          const href = targetHref(e);
          return (
            <TR key={e.id}>
              <TD className="whitespace-nowrap text-fg-muted">
                <time dateTime={e.created_at} title={formatDateTime(e.created_at)}>
                  {formatRelative(e.created_at)}
                </time>
              </TD>
              <TD className="whitespace-nowrap">
                <code className="rounded-xs bg-sunken px-1.5 py-0.5 type-caption text-fg">{e.action}</code>
              </TD>
              {showTarget && (
                <TD className="whitespace-nowrap">
                  <span className="flex items-center gap-2">
                    <span className="text-fg-muted">{e.target_type}</span>
                    {href ? (
                      <Link href={href} className="font-semibold text-fg underline-offset-2 hover:underline">
                        {shortId(e.target_id)}
                      </Link>
                    ) : (
                      <span className="font-semibold">{shortId(e.target_id)}</span>
                    )}
                    {onFilterTarget && <IconButton label="Show only this target" tooltip icon={<Filter />} size="sm" onClick={() => onFilterTarget(e)} />}
                  </span>
                </TD>
              )}
              <TD className="whitespace-nowrap">{e.admin_email ?? <span className="text-fg-muted">System</span>}</TD>
              <TD>
                <DataCell data={e.data} />
              </TD>
            </TR>
          );
        })}
      </tbody>
    </Table>
  );
}
