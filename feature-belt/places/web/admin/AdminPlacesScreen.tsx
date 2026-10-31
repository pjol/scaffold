"use client";

import { errorMessage, useAdminPlaces, type PlaceStatus } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, SegmentedControl } from "@scaffoldapp/ui-web";
import { Inbox, Plus, SearchX } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { useCategoryLabel } from "../parts";

const STATUSES: { value: PlaceStatus; label: string }[] = [
  { value: "pending", label: "In review" },
  { value: "approved", label: "Approved" },
  { value: "rejected", label: "Rejected" },
];

/** Admin → Places: the review queue (oldest first), approved and rejected listings, search. */
export function AdminPlacesScreen() {
  const router = useRouter();
  const [status, setStatus] = useUrlState<PlaceStatus>("status", "pending");
  const [q, setQ] = useUrlState("q", "");
  const places = useAdminPlaces({ status, q: q || undefined, limit: 50 });
  const label = useCategoryLabel();
  const rows = places.data?.items ?? [];

  return (
    <div className="flex flex-col gap-section">
      <PageHeader
        title="Places"
        action={
          <Button asChild size="sm" leftIcon={<Plus />}>
            <Link href="/admin/places/new">Add place</Link>
          </Button>
        }
      />
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-3">
          <div className="self-start">
            <SegmentedControl options={STATUSES} value={status} onValueChange={(v) => setStatus(v)} aria-label="Status" />
          </div>
          <SearchInput value={q} onSearch={setQ} placeholder="Name, address or email" label="Search places" className="sm:max-w-sm" />
        </div>
        <Card padding="none">
          {places.isPending ? (
            <TableSkeleton rows={6} cols={4} />
          ) : places.isError ? (
            <ErrorState compact title="Couldn't load places" info={errorMessage(places.error)} onRetry={() => void places.refetch()} retrying={places.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={q ? <SearchX /> : <Inbox />} title={q ? "No matching places" : status === "pending" ? "Nothing to review" : "No places"} />
          ) : (
            <>
              <Table label="Places">
                <THead>
                  <TH>Place</TH>
                  <TH className="hidden sm:table-cell">Category</TH>
                  <TH className="hidden md:table-cell">Submitted by</TH>
                  <TH align="right">{status === "pending" ? "Waiting since" : "Added"}</TH>
                </THead>
                <tbody>
                  {rows.map((p) => (
                    <TR key={p.id} onActivate={() => router.push(`/admin/places/${p.id}`)}>
                      <TD>
                        <Link href={`/admin/places/${p.id}`} className="font-semibold text-fg underline-offset-2 hover:underline">
                          {p.name}
                        </Link>
                      </TD>
                      <TD className="hidden text-fg-muted sm:table-cell">{label(p.category)}</TD>
                      <TD className="hidden max-w-64 truncate text-fg-muted md:table-cell">{p.owner ? p.owner.display_name || p.owner.email : "Staff"}</TD>
                      <TD align="right" className="whitespace-nowrap text-fg-muted">
                        {formatDate(p.created_at)}
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
              <LoadMore hasNextPage={places.hasNextPage} isFetchingNextPage={places.isFetchingNextPage} fetchNextPage={places.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
    </div>
  );
}
