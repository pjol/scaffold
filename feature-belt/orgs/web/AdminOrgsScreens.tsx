"use client";

import { useAdminOrg, useAdminOrgs } from "@scaffoldapp/shared";
import { Badge, Card, EmptyState, ErrorState, ListRow, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { Building2, ChevronLeft, SearchX } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import type { ReactNode } from "react";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { ROLE_LABEL, ROLE_TONE } from "./labels";

/** Admin → Organizations: search by name, URL name or id; newest first. */
export function AdminOrgsScreen() {
  const router = useRouter();
  const [q, setQ] = useUrlState("q", "");
  const [memberId] = useUrlState("member_id", "");
  const orgs = useAdminOrgs({ ...(q ? { q } : {}), ...(memberId ? { member_id: memberId } : {}), limit: 50 });
  const rows = orgs.data?.items ?? [];
  const filtered = !!q || !!memberId;
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Organizations" />
      <div className="flex flex-col gap-4">
        <SearchInput value={q} onSearch={setQ} placeholder="Name, URL name or ID" label="Search organizations" className="sm:max-w-sm" />
        <Card padding="none">
          {orgs.isPending ? (
            <TableSkeleton rows={6} cols={3} />
          ) : orgs.isError ? (
            <ErrorState compact title="Couldn't load organizations" info={orgs.error.message} onRetry={() => void orgs.refetch()} retrying={orgs.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={filtered ? <SearchX /> : <Building2 />} title={filtered ? "No matching organizations" : "No organizations yet"} />
          ) : (
            <>
              <Table label="Organizations">
                <THead>
                  <TH>Organization</TH>
                  <TH className="hidden sm:table-cell">URL name</TH>
                  <TH align="right">Members</TH>
                  <TH className="hidden md:table-cell" align="right" info="Pending email invites that haven't expired.">
                    Invites
                  </TH>
                  <TH className="hidden lg:table-cell" align="right">
                    Created
                  </TH>
                </THead>
                <tbody>
                  {rows.map((o) => (
                    <TR key={o.id} onActivate={() => router.push(`/admin/orgs/${o.id}`)}>
                      <TD>
                        <span className="flex min-w-0 items-center gap-3">
                          <AppAvatar name={o.name} src={o.avatar_url} size="sm" />
                          <Link href={`/admin/orgs/${o.id}`} className="truncate font-semibold text-fg underline-offset-2 hover:underline">
                            {o.name}
                          </Link>
                        </span>
                      </TD>
                      <TD className="hidden text-fg-muted sm:table-cell">{o.slug}</TD>
                      <TD align="right">{o.member_count}</TD>
                      <TD className="hidden md:table-cell" align="right">
                        {o.pending_invites}
                      </TD>
                      <TD className="hidden whitespace-nowrap text-fg-muted lg:table-cell" align="right">
                        {formatDate(o.created_at)}
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
              <LoadMore hasNextPage={orgs.hasNextPage} isFetchingNextPage={orgs.isFetchingNextPage} fetchNextPage={orgs.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
    </div>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-4">
      <dt className="type-label text-fg-muted sm:w-32 sm:shrink-0">{label}</dt>
      <dd className="min-w-0 type-body text-fg">{children}</dd>
    </div>
  );
}

function BackToOrgs() {
  return (
    <Link href="/admin/orgs" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
      <ChevronLeft className="size-4" aria-hidden />
      Organizations
    </Link>
  );
}

/** Admin → organization: facts plus its owners and admins (links to their user pages). */
export function AdminOrgScreen({ id }: { id: string }) {
  const org = useAdminOrg(id);
  if (org.isPending) {
    return (
      <SkeletonGroup label="Loading organization" className="flex flex-col gap-section">
        <Skeleton className="h-10 w-32" />
        <Skeleton className="h-14 w-72" />
        <Skeleton className="h-40" />
      </SkeletonGroup>
    );
  }
  if (org.isError) {
    return (
      <div className="flex flex-col gap-2">
        <BackToOrgs />
        <Card padding="compact">
          {org.error.status === 404 ? (
            <EmptyState compact icon={<Building2 />} title="Organization not found" />
          ) : (
            <ErrorState compact title="Couldn't load this organization" info={org.error.message} onRetry={() => void org.refetch()} retrying={org.isFetching} />
          )}
        </Card>
      </div>
    );
  }
  const o = org.data;
  return (
    <div className="flex flex-col gap-2">
      <BackToOrgs />
      <div className="flex flex-col gap-section">
        <div className="flex flex-wrap items-center gap-4">
          <AppAvatar name={o.name} src={o.avatar_url} size="lg" eager />
          <h1 className="min-w-0 type-title text-fg">{o.name}</h1>
        </div>
        <Section title="Details">
          <Card>
            <dl className="flex flex-col gap-3">
              <Fact label="URL name">{o.slug}</Fact>
              <Fact label="Members">{o.member_count}</Fact>
              <Fact label="Pending invites">{o.pending_invites}</Fact>
              <Fact label="Created">{formatDate(o.created_at)}</Fact>
              <Fact label="ID">
                <code className="break-all type-label text-fg-muted">{o.id}</code>
              </Fact>
            </dl>
          </Card>
        </Section>
        <Section title="Owners and admins">
          <Card padding="none" className="p-1">
            {o.leaders.length === 0 ? (
              <EmptyState compact icon={<Building2 />} title="No owners" />
            ) : (
              <ul className="flex flex-col">
                {o.leaders.map((m) => (
                  <li key={m.user_id}>
                    <ListRow
                      asChild
                      title={m.display_name || m.email || "Member"}
                      leading={<AppAvatar name={m.display_name || m.email || "?"} src={m.avatar_url} size="md" />}
                      trailing={<Badge tone={ROLE_TONE[m.role]}>{ROLE_LABEL[m.role]}</Badge>}
                    >
                      <Link href={`/admin/users/${m.user_id}`} />
                    </ListRow>
                  </li>
                ))}
              </ul>
            )}
          </Card>
        </Section>
      </div>
    </div>
  );
}
