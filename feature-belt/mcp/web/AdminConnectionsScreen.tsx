"use client";

import { errorMessage, useAdminMcpConnections, useAdminRevokeMcpConnection, type McpAdminConnection } from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, EmptyState, ErrorState, Select, toast } from "@scaffoldapp/ui-web";
import { Plug, SearchX } from "lucide-react";
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
  { value: "revoked", label: "Ended" },
];
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

const REASONS: Record<NonNullable<McpAdminConnection["revoke_reason"]>, string> = {
  user: "Disconnected by user",
  staff: "Revoked by staff",
  client: "Revoked by the app",
  refresh_reuse: "Refresh token replayed",
  code_reuse: "Code replayed",
};

function statusOf(c: McpAdminConnection): { label: string; tone: "neutral" | "danger" | "warning" } {
  if (c.revoked_at) return { label: c.revoke_reason ? REASONS[c.revoke_reason] : "Revoked", tone: c.revoke_reason?.endsWith("reuse") ? "danger" : "neutral" };
  if (new Date(c.expires_at).getTime() <= Date.now()) return { label: "Lapsed", tone: "neutral" };
  return { label: c.scopes.includes("write") ? "Read and write" : "Read only", tone: c.scopes.includes("write") ? "warning" : "neutral" };
}

/** Admin → Connected apps: every OAuth grant, newest first; revoke is audited (mcp.oauth.revoke). */
export function AdminConnectionsScreen() {
  const [user, setUser] = useUrlState("user", "");
  const [status, setStatus] = useUrlState<StatusFilter>("status", "active");
  const userId = UUID.test(user.trim()) ? user.trim() : undefined;
  const list = useAdminMcpConnections({ ...(userId ? { user_id: userId } : {}), ...(status !== "any" ? { status } : {}), limit: 50 });
  const revoke = useAdminRevokeMcpConnection();
  const [revoking, setRevoking] = useState<McpAdminConnection | null>(null);
  const rows = list.data?.items ?? [];
  const filtered = !!userId || status !== "any";
  const badUser = user.trim() !== "" && !userId;

  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Connected apps" info="Agents people connected by signing in (OAuth). One row per sign-in; revoking ends its tokens at once." />
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2 sm:flex-row">
          <SearchInput value={user} onSearch={setUser} placeholder="User ID" label="Filter by user ID" className="sm:max-w-sm sm:flex-1" />
          <Select aria-label="Status" options={STATUS_OPTIONS} value={status} onValueChange={(v) => setStatus(v as StatusFilter)} className="sm:w-48" />
        </div>
        <Card padding="none">
          {badUser ? (
            <EmptyState compact icon={<SearchX />} title="Enter a full user ID" />
          ) : list.isPending ? (
            <TableSkeleton rows={6} cols={5} />
          ) : list.isError ? (
            <ErrorState compact title="Couldn't load connected apps" onRetry={() => void list.refetch()} retrying={list.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={filtered ? <SearchX /> : <Plug />} title={filtered ? "No matching connections" : "No connected apps yet"} />
          ) : (
            <>
              <Table label="Connected apps" className="min-w-160">
                <THead>
                  <TH>App</TH>
                  <TH>Owner</TH>
                  <TH>Access</TH>
                  <TH align="right">Last used</TH>
                  <TH align="right">Connected</TH>
                  <TH align="right">
                    <span className="sr-only">Actions</span>
                  </TH>
                </THead>
                <tbody>
                  {rows.map((c) => {
                    const st = statusOf(c);
                    const live = !c.revoked_at && new Date(c.expires_at).getTime() > Date.now();
                    return (
                      <TR key={c.id}>
                        <TD>
                          <span className="flex flex-col">
                            <span className="font-semibold">{c.client_name || "Unnamed app"}</span>
                            {c.client_host ? <span className="type-caption text-fg-muted">{c.client_host}</span> : null}
                          </span>
                        </TD>
                        <TD className="max-w-56 truncate">
                          <Link href={`/admin/users/${c.user_id}`} className="underline-offset-2 hover:underline">
                            {c.user_email}
                          </Link>
                        </TD>
                        <TD>
                          <Badge size="sm" tone={st.tone}>
                            {st.label}
                          </Badge>
                        </TD>
                        <TD align="right" className="whitespace-nowrap text-fg-muted">
                          {c.last_used_at ? formatRelative(c.last_used_at) : "Never"}
                        </TD>
                        <TD align="right" className="whitespace-nowrap text-fg-muted">
                          {formatDate(c.created_at)}
                        </TD>
                        <TD align="right">
                          {live ? (
                            <Button size="sm" variant="danger-ghost" onClick={() => setRevoking(c)}>
                              Revoke
                            </Button>
                          ) : null}
                        </TD>
                      </TR>
                    );
                  })}
                </tbody>
              </Table>
              <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(o) => !o && setRevoking(null)}
        title={`Revoke ${revoking?.client_name || "this app"}?`}
        confirmLabel="Revoke"
        tone="danger"
        onConfirm={async () => {
          if (!revoking) return;
          await revoke.mutateAsync(revoking.id);
          toast.success("Connection revoked");
          setRevoking(null);
        }}
        errorText={(e) => errorMessage(e, "Couldn't revoke the connection. Try again.")}
      >
        {revoking ? `${revoking.user_email}'s app stops working right away. The action is recorded in the audit log.` : null}
      </ConfirmDialog>
    </div>
  );
}
