"use client";

import { errorMessage, hasErrorCode, orgsNounTitle, useActiveOrg, useCancelOrgJoinRequest, useJoinableOrgs, useJoinOrgByDomain, useRequestToJoinOrg, type JoinableOrg, type Org } from "@scaffoldapp/shared";
import { Badge, Button, Card, ListRow, toast } from "@scaffoldapp/ui-web";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { Section } from "@/components/shell/PageHeader";

function info(j: JoinableOrg): string {
  const members = `${j.member_count} ${j.member_count === 1 ? "member" : "members"}`;
  if (j.action === "requested") return `You asked to join. An admin of ${j.org.name} decides. ${members}.`;
  if (j.action === "join") return `Anyone with a verified ${j.domain} email can join. ${members}.`;
  return `People with a verified ${j.domain} email can ask to join; an admin approves. ${members}.`;
}

/**
 * "Organizations you can join": orgs whose verified domain matches your verified email.
 * Renders nothing while loading or when there are none. Settings → Organizations shows it;
 * an onboarding screen for people without an org can render it too.
 */
export function JoinableOrgs({ onJoined }: { onJoined?: (org: Org) => void }) {
  const joinable = useJoinableOrgs();
  const join = useJoinOrgByDomain();
  const request = useRequestToJoinOrg();
  const cancel = useCancelOrgJoinRequest();
  const { setActive } = useActiveOrg();
  const rows = joinable.data?.items ?? [];
  if (joinable.isPending || joinable.isError || rows.length === 0) return null;

  const onJoin = (j: JoinableOrg) =>
    join.mutate(j.org.id, {
      onSuccess: (org) => {
        setActive(org.id);
        toast.success(`You joined ${org.name}`);
        onJoined?.(org);
      },
      onError: (e) => {
        toast.error(errorMessage(e, "Couldn't join. Try again."));
        if (hasErrorCode(e, "approval_required")) void joinable.refetch();
      },
    });
  const onRequest = (j: JoinableOrg) =>
    request.mutate(j.org.id, {
      onSuccess: () => toast.success(`Asked to join ${j.org.name}`),
      onError: (e) => toast.error(errorMessage(e, "Couldn't send the request. Try again.")),
    });

  return (
    <Section title={`${orgsNounTitle("many")} you can join`} info="Matched by the domain of your verified email address.">
      <Card padding="none" className="p-1">
        <ul className="flex flex-col">
          {rows.map((j) => (
            <li key={j.org.id} className="flex flex-col sm:flex-row sm:items-center">
              <ListRow
                className="min-w-0 flex-1"
                title={j.org.name}
                leading={<AppAvatar name={j.org.name} src={j.org.avatar_url} size="md" />}
                info={info(j)}
                {...(j.action === "requested" ? { trailing: <Badge tone="info">Requested</Badge> } : {})}
              />
              <span className="flex gap-2 px-4 pb-3 sm:p-0 sm:pr-4">
                {j.action === "requested" ? (
                  <Button
                    className="flex-1 sm:flex-none"
                    variant="ghost"
                    size="sm"
                    loading={cancel.isPending && cancel.variables === j.request_id}
                    onClick={() =>
                      j.request_id && cancel.mutate(j.request_id, { onSuccess: () => toast.success("Request withdrawn"), onError: (e) => toast.error(errorMessage(e, "Couldn't withdraw. Try again.")) })
                    }
                  >
                    Withdraw
                  </Button>
                ) : j.action === "join" ? (
                  <Button className="flex-1 sm:flex-none" size="sm" loading={join.isPending && join.variables === j.org.id} onClick={() => onJoin(j)}>
                    Join
                  </Button>
                ) : (
                  <Button className="flex-1 sm:flex-none" variant="secondary" size="sm" loading={request.isPending && request.variables === j.org.id} onClick={() => onRequest(j)}>
                    Ask to join
                  </Button>
                )}
              </span>
            </li>
          ))}
        </ul>
      </Card>
    </Section>
  );
}
