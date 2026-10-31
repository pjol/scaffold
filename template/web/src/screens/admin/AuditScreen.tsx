"use client";

import { useAdminAudit } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState } from "@scaffoldapp/ui-web";
import { ScrollText, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { AuditTable } from "@/components/admin/AuditTable";
import { SearchInput } from "@/components/admin/SearchInput";
import { TableSkeleton } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";

/** Admin → Audit log: every admin mutation, newest first; filter by target type and id (kept in the URL). */
export function AuditScreen() {
  const router = useRouter();
  const [type, setType] = useUrlState("target_type", "");
  const [target, setTarget] = useUrlState("target_id", "");
  const audit = useAdminAudit({ target_type: type || undefined, target_id: target || undefined, limit: 50 });
  const entries = audit.data?.items ?? [];
  const filtered = !!type || !!target;

  return (
    <Screen title="Audit log" info="Written in the same transaction as each admin change.">
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <SearchInput value={type} onSearch={setType} placeholder="Target type, e.g. user" label="Target type" className="sm:w-56" />
          <SearchInput value={target} onSearch={setTarget} placeholder="Target ID" label="Target ID" className="sm:max-w-sm sm:flex-1" />
          {filtered && (
            <Button variant="ghost" leftIcon={<X />} className="self-start sm:self-auto" onClick={() => router.replace("/admin/audit", { scroll: false })}>
              Clear
            </Button>
          )}
        </div>
        <Card padding="none">
          {audit.isPending ? (
            <TableSkeleton rows={8} cols={5} />
          ) : audit.isError ? (
            <ErrorState compact title="Couldn't load the audit log" info={audit.error.message} onRetry={() => void audit.refetch()} retrying={audit.isFetching} />
          ) : entries.length === 0 ? (
            <EmptyState compact icon={<ScrollText />} title={filtered ? "No matching entries" : "No admin actions yet"} />
          ) : (
            <>
              <AuditTable
                entries={entries}
                onFilterTarget={(e) => router.replace(`/admin/audit?target_type=${encodeURIComponent(e.target_type)}&target_id=${encodeURIComponent(e.target_id)}`, { scroll: false })}
              />
              <LoadMore hasNextPage={audit.hasNextPage} isFetchingNextPage={audit.isFetchingNextPage} fetchNextPage={audit.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
    </Screen>
  );
}
