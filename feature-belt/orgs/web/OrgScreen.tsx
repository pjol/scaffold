"use client";

import { orgRoleAtLeast, orgsNoun, orgsNounTitle, topic, useOrg, useTopic } from "@scaffoldapp/shared";
import { Badge, Card, EmptyState, ErrorState, Skeleton, SkeletonGroup, Tab, TabPanel, Tabs, TabsList } from "@scaffoldapp/ui-web";
import { Building2 } from "lucide-react";
import Link from "next/link";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { useUrlState } from "@/lib/url-state";
import { ROLE_LABEL, ROLE_TONE } from "./labels";
import { OrgDomains } from "./OrgDomains";
import { OrgGeneral } from "./OrgGeneral";
import { OrgInvites } from "./OrgInvites";
import { OrgMembers } from "./OrgMembers";

type TabId = "general" | "members" | "invites" | "domains";

/** Settings → Organizations → one org: General, Members and (admins) Invites and Domains, with the tab in the URL. */
export function OrgScreen({ orgId }: { orgId: string }) {
  const org = useOrg(orgId);
  const [tab, setTab] = useUrlState<TabId>("tab", "general");
  useTopic(org.data ? topic("orgs", orgId) : null);

  if (org.isPending) {
    return (
      <SkeletonGroup label={`Loading ${orgsNoun()}`} className="flex flex-col gap-section">
        <div className="flex items-center gap-4">
          <Skeleton shape="circle" className="size-14" />
          <Skeleton className="h-8 w-56" />
        </div>
        <Skeleton className="h-10 w-72" />
        <Skeleton className="h-48" />
      </SkeletonGroup>
    );
  }
  if (org.isError) {
    return (
      <Card padding="compact">
        {org.error.status === 404 ? (
          <EmptyState
            compact
            icon={<Building2 />}
            title={`${orgsNounTitle()} not found`}
            action={
              <Link href="/settings/orgs" className="type-label font-semibold underline underline-offset-2">
                Your {orgsNoun("many")}
              </Link>
            }
          />
        ) : (
          <ErrorState compact title={`Couldn't load this ${orgsNoun()}`} info={org.error.message} onRetry={() => void org.refetch()} retrying={org.isFetching} />
        )}
      </Card>
    );
  }

  const o = org.data;
  const admin = orgRoleAtLeast(o.role, "admin");
  const current: TabId = (tab === "invites" || tab === "domains") && !admin ? "general" : tab;
  return (
    <div className="flex flex-col gap-section">
      <div className="flex min-h-control-lg flex-wrap items-center gap-4">
        <AppAvatar name={o.name} src={o.avatar_url} size="lg" eager />
        <h1 className="min-w-0 flex-1 truncate type-title text-fg">{o.name}</h1>
        <Badge tone={ROLE_TONE[o.role]}>{ROLE_LABEL[o.role]}</Badge>
      </div>
      <Tabs value={current} onValueChange={(v) => setTab(v as TabId)}>
        <TabsList aria-label={orgsNounTitle()}>
          <Tab value="general">General</Tab>
          <Tab value="members" count={o.member_count}>
            Members
          </Tab>
          {admin && <Tab value="invites">Invites</Tab>}
          {admin && <Tab value="domains">Domains</Tab>}
        </TabsList>
        <TabPanel value="general" className="pt-8">
          <OrgGeneral org={o} />
        </TabPanel>
        <TabPanel value="members" className="pt-8">
          <OrgMembers org={o} />
        </TabPanel>
        {admin && (
          <TabPanel value="invites" className="pt-8">
            <OrgInvites org={o} />
          </TabPanel>
        )}
        {admin && (
          <TabPanel value="domains" className="pt-8">
            <OrgDomains org={o} />
          </TabPanel>
        )}
      </Tabs>
    </div>
  );
}
