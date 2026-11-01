"use client";

import { useAdminUsers, type UserStatus } from "@scaffoldapp/shared";
import { Card, EmptyState, ErrorState, Select } from "@scaffoldapp/ui-web";
import { SearchX, Users } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { RoleBadge, STATUS_LABEL, StatusBadge } from "@/components/admin/badges";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";

type StatusFilter = "any" | UserStatus;

const STATUS_OPTIONS = [
  { value: "any", label: "Any status" },
  ...(Object.keys(STATUS_LABEL) as UserStatus[]).map((s) => ({ value: s, label: STATUS_LABEL[s] })),
];

/** Admin → Users: search (name, email or exact id), status filter, newest first, keyset pages. Filters live in the URL. */
export function UsersScreen() {
  const router = useRouter();
  const [q, setQ] = useUrlState("q", "");
  const [status, setStatus] = useUrlState<StatusFilter>("status", "any");
  const users = useAdminUsers({ q: q || undefined, status: status === "any" ? undefined : status, limit: 50 });
  const rows = users.data?.items ?? [];
  const filtered = !!q || status !== "any";

  return (
    <Screen title="Users">
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2 sm:flex-row">
          <SearchInput value={q} onSearch={setQ} placeholder="Name, email or ID" label="Search users" className="sm:max-w-sm sm:flex-1" />
          <Select aria-label="Status" options={STATUS_OPTIONS} value={status} onValueChange={(v) => setStatus(v as StatusFilter)} className="sm:w-48" />
        </div>
        <Card padding="none">
          {users.isPending ? (
            <TableSkeleton rows={8} cols={4} />
          ) : users.isError ? (
            <ErrorState compact title="Couldn't load users" info={users.error.message} onRetry={() => void users.refetch()} retrying={users.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={filtered ? <SearchX /> : <Users />} title={filtered ? "No matching users" : "No users yet"} />
          ) : (
            <>
              <Table label="Users">
                <THead>
                  <TH>User</TH>
                  <TH className="hidden sm:table-cell">Email</TH>
                  <TH className="hidden md:table-cell">Role</TH>
                  <TH>Status</TH>
                  <TH className="hidden lg:table-cell" align="right">
                    Joined
                  </TH>
                </THead>
                <tbody>
                  {rows.map((u) => {
                    const name = u.display_name || (u.status === "deleted" ? "Deleted user" : u.email);
                    return (
                      <TR key={u.id} onActivate={() => router.push(`/admin/users/${u.id}`)}>
                        <TD>
                          <span className="flex min-w-0 items-center gap-3">
                            <AppAvatar name={name} src={u.avatar_url} size="sm" />
                            <Link href={`/admin/users/${u.id}`} className="truncate font-semibold text-fg underline-offset-2 hover:underline">
                              {name}
                            </Link>
                          </span>
                        </TD>
                        <TD className="hidden max-w-64 truncate text-fg-muted sm:table-cell">{u.status === "deleted" ? "" : u.email}</TD>
                        <TD className="hidden md:table-cell">
                          <RoleBadge role={u.admin_role} />
                        </TD>
                        <TD>
                          <StatusBadge status={u.status} />
                        </TD>
                        <TD className="hidden whitespace-nowrap text-fg-muted lg:table-cell" align="right">
                          {formatDate(u.created_at)}
                        </TD>
                      </TR>
                    );
                  })}
                </tbody>
              </Table>
              <LoadMore hasNextPage={users.hasNextPage} isFetchingNextPage={users.isFetchingNextPage} fetchNextPage={users.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
    </Screen>
  );
}
