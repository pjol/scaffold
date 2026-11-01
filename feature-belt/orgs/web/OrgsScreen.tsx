"use client";

import { errorMessage, orgsNoun, orgsNounTitle, useAcceptMyInvite, useActiveOrg, useDeclineMyInvite, useMyInvites, useOrgPickerVisible, useOrgs } from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, ListRow, SkeletonGroup, SkeletonRow, Stagger, StaggerItem, toast } from "@scaffoldapp/ui-web";
import { Building2, Plus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { CreateOrgDialog } from "./CreateOrgDialog";
import { JoinableOrgs } from "./JoinableOrgs";
import { ROLE_LABEL, ROLE_TONE } from "./labels";
import { OrgSwitcher } from "./OrgSwitcher";

function Invitations() {
  const router = useRouter();
  const invites = useMyInvites();
  const accept = useAcceptMyInvite();
  const decline = useDeclineMyInvite();
  const { setActive } = useActiveOrg();
  const rows = invites.data?.items ?? [];
  if (invites.isPending || invites.isError || rows.length === 0) return null;
  return (
    <Section title="Invitations">
      <Card padding="none" className="p-1">
        <ul className="flex flex-col">
          {rows.map((inv) => (
            <li key={inv.id}>
              <ListRow
                title={inv.org.name}
                leading={<AppAvatar name={inv.org.name} src={inv.org.avatar_url} size="md" />}
                info={`${inv.invited_by_name ? `${inv.invited_by_name} invited you` : "You were invited"} as ${inv.role === "admin" ? "an admin" : "a member"}. The invite expires ${formatDate(inv.expires_at)}.`}
                trailing={
                  <span className="flex gap-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={accept.isPending}
                      onClick={() => decline.mutate(inv.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't decline. Try again.")) })}
                    >
                      Decline
                    </Button>
                    <Button
                      size="sm"
                      loading={accept.isPending && accept.variables === inv.id}
                      onClick={() =>
                        accept.mutate(inv.id, {
                          onSuccess: (org) => {
                            setActive(org.id);
                            toast.success(`You joined ${org.name}`);
                            router.push(`/settings/orgs/${org.id}`);
                          },
                          onError: (e) => toast.error(errorMessage(e, "Couldn't accept. Try again.")),
                        })
                      }
                    >
                      Join
                    </Button>
                  </span>
                }
              />
            </li>
          ))}
        </ul>
      </Card>
    </Section>
  );
}

/** Settings → Organizations: invitations to you, orgs your email domain lets you join, the active org, and every org you belong to. */
export function OrgsScreen() {
  const orgs = useOrgs();
  const { org: active } = useActiveOrg();
  const picker = useOrgPickerVisible();
  const [creating, setCreating] = useState(false);
  const rows = orgs.data?.items ?? [];

  return (
    <div className="flex flex-col gap-section">
      <PageHeader
        title={orgsNounTitle("many")}
        action={
          rows.length > 0 ? (
            <Button variant="secondary" size="auto" leftIcon={<Plus />} onClick={() => setCreating(true)}>
              New
            </Button>
          ) : undefined
        }
      />
      <Invitations />
      <JoinableOrgs />
      {rows.length > 1 && picker && (
        <Section title={`Active ${orgsNoun()}`} info={`Screens that work per ${orgsNoun()} show this one. The choice is kept in this browser.`}>
          <OrgSwitcher className="self-start" />
        </Section>
      )}
      <Section title={`Your ${orgsNoun("many")}`}>
        <Card padding="none" className="p-1">
          {orgs.isPending ? (
            <SkeletonGroup label={`Loading ${orgsNoun("many")}`} className="flex flex-col">
              <SkeletonRow />
              <SkeletonRow />
            </SkeletonGroup>
          ) : orgs.isError ? (
            <ErrorState compact title={`Couldn't load your ${orgsNoun("many")}`} info={orgs.error.message} onRetry={() => void orgs.refetch()} retrying={orgs.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState
              compact
              icon={<Building2 />}
              title={`No ${orgsNoun("many")} yet`}
              action={
                <Button size="auto" leftIcon={<Plus />} onClick={() => setCreating(true)}>
                  New {orgsNoun()}
                </Button>
              }
            />
          ) : (
            <Stagger as="ul" className="flex flex-col">
              {rows.map((o) => (
                <StaggerItem as="li" key={o.id}>
                  <ListRow
                    asChild
                    title={o.name}
                    leading={<AppAvatar name={o.name} src={o.avatar_url} size="md" />}
                    trailing={
                      <span className="flex items-center gap-2">
                        {active?.id === o.id && rows.length > 1 && <Badge tone="success">Active</Badge>}
                        <Badge tone={ROLE_TONE[o.role]}>{ROLE_LABEL[o.role]}</Badge>
                      </span>
                    }
                  >
                    <Link href={`/settings/orgs/${o.id}`} />
                  </ListRow>
                </StaggerItem>
              ))}
            </Stagger>
          )}
          <LoadMore hasNextPage={orgs.hasNextPage} isFetchingNextPage={orgs.isFetchingNextPage} fetchNextPage={orgs.fetchNextPage} />
        </Card>
      </Section>
      <CreateOrgDialog open={creating} onOpenChange={setCreating} />
    </div>
  );
}
