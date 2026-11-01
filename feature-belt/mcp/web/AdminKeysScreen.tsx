"use client";

import { errorMessage, useAdminMcpKeys, useAdminRevokeMcpKey, type McpAdminKey } from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, EmptyState, ErrorState, Select, toast } from "@scaffoldapp/ui-web";
import { KeyRound, SearchX } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { formatDate, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";

type StatusFilter = "any" | "active" | "revoked";
const STATUS_OPTIONS = [
  { value: "any", label: "Any status" },
  { value: "active", label: "Active" },
  { value: "revoked", label: "Revoked" },
];
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Admin → API keys: every user's keys, newest first; revoke is audited (mcp.key.revoke). */
export function AdminKeysScreen() {
  const [user, setUser] = useUrlState("user", "");
  const [status, setStatus] = useUrlState<StatusFilter>("status", "active");
  const userId = UUID.test(user.trim()) ? user.trim() : undefined;
  const keys = useAdminMcpKeys({ ...(userId ? { user_id: userId } : {}), ...(status !== "any" ? { status } : {}), limit: 50 });
  const revoke = useAdminRevokeMcpKey();
  const [revoking, setRevoking] = useState<McpAdminKey | null>(null);
  const rows = keys.data?.items ?? [];
  const filtered = !!userId || status !== "any";
  const badUser = user.trim() !== "" && !userId;

  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="API keys" info="Personal keys agents use to call the app's tools over MCP. Revoking stops the key at once." />
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2 sm:flex-row">
          <SearchInput value={user} onSearch={setUser} placeholder="User ID" label="Filter by user ID" className="sm:max-w-sm sm:flex-1" />
          <Select aria-label="Status" options={STATUS_OPTIONS} value={status} onValueChange={(v) => setStatus(v as StatusFilter)} className="sm:w-48" />
        </div>
        <Card padding="none">
          {badUser ? (
            <EmptyState compact icon={<SearchX />} title="Enter a full user ID" />
          ) : keys.isPending ? (
            <TableSkeleton rows={6} cols={5} />
          ) : keys.isError ? (
            <ErrorState compact title="Couldn't load keys" onRetry={() => void keys.refetch()} retrying={keys.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={filtered ? <SearchX /> : <KeyRound />} title={filtered ? "No matching keys" : "No API keys yet"} />
          ) : (
            <>
              <Table label="API keys" className="min-w-160">
                <THead>
                  <TH>Key</TH>
                  <TH>Owner</TH>
                  <TH>Access</TH>
                  <TH align="right">Last used</TH>
                  <TH align="right">Created</TH>
                  <TH align="right">
                    <span className="sr-only">Actions</span>
                  </TH>
                </THead>
                <tbody>
                  {rows.map((k) => (
                    <TR key={k.id}>
                      <TD>
                        <span className="flex flex-col">
                          <span className="font-semibold">{k.name}</span>
                          <code className="font-mono type-caption text-fg-muted">{k.prefix}…</code>
                        </span>
                      </TD>
                      <TD className="max-w-56 truncate">
                        <Link href={`/admin/users/${k.user_id}`} className="underline-offset-2 hover:underline">
                          {k.user_email}
                        </Link>
                      </TD>
                      <TD>
                        {k.revoked_at ? (
                          <Badge size="sm" tone="neutral">
                            Revoked
                          </Badge>
                        ) : (
                          <Badge size="sm" tone={k.scopes.includes("write") ? "warning" : "neutral"}>
                            {k.scopes.includes("write") ? "Read and write" : "Read only"}
                          </Badge>
                        )}
                      </TD>
                      <TD align="right" className="whitespace-nowrap text-fg-muted">
                        {k.last_used_at ? formatRelative(k.last_used_at) : "Never"}
                      </TD>
                      <TD align="right" className="whitespace-nowrap text-fg-muted">
                        {formatDate(k.created_at)}
                      </TD>
                      <TD align="right">
                        {k.revoked_at ? null : (
                          <Button size="sm" variant="danger-ghost" onClick={() => setRevoking(k)}>
                            Revoke
                          </Button>
                        )}
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
              <LoadMore hasNextPage={keys.hasNextPage} isFetchingNextPage={keys.isFetchingNextPage} fetchNextPage={keys.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(o) => !o && setRevoking(null)}
        title={`Revoke ${revoking?.name ?? "this key"}?`}
        confirmLabel="Revoke"
        tone="danger"
        onConfirm={async () => {
          if (!revoking) return;
          await revoke.mutateAsync(revoking.id);
          toast.success("Key revoked");
          setRevoking(null);
        }}
        errorText={(e) => errorMessage(e, "Couldn't revoke the key. Try again.")}
      >
        {revoking ? `${revoking.user_email}'s agents using it stop working right away. The action is recorded in the audit log.` : null}
      </ConfirmDialog>
    </div>
  );
}
