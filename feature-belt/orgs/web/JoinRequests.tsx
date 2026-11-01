"use client";

import { errorMessage, useApproveOrgJoinRequest, useDenyOrgJoinRequest, useOrgJoinRequests, type Org } from "@scaffoldapp/shared";
import { Button, Card, ErrorState, ListRow, toast } from "@scaffoldapp/ui-web";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { Section } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";

/**
 * Requests to join (admins), above the member list. Shows nothing while loading or when
 * there are none, so orgs without a request-policy domain never see it.
 */
export function JoinRequests({ org }: { org: Org }) {
  const requests = useOrgJoinRequests(org.id);
  const approve = useApproveOrgJoinRequest(org.id);
  const deny = useDenyOrgJoinRequest(org.id);
  const rows = requests.data?.items ?? [];
  if (requests.isPending || (!requests.isError && rows.length === 0)) return null;
  return (
    <Section title="Requests to join" info="People whose verified email is on one of your domains. Approved people join as members; declined people can ask again after 7 days.">
      <Card padding="none" className="p-1">
        {requests.isError ? (
          <ErrorState compact title="Couldn't load requests" info={requests.error.message} onRetry={() => void requests.refetch()} retrying={requests.isFetching} />
        ) : (
          <ul className="flex flex-col">
            {rows.map((j) => {
              const name = j.display_name || j.email;
              const busy = (approve.isPending && approve.variables === j.id) || (deny.isPending && deny.variables === j.id);
              return (
                <li key={j.id} className="flex flex-col sm:flex-row sm:items-center">
                  <ListRow className="min-w-0 flex-1" title={name} leading={<AppAvatar name={name} src={j.avatar_url} size="md" />} info={`${j.email} asked on ${formatDate(j.created_at)}.`} />
                  <span className="flex gap-2 px-4 pb-3 sm:p-0 sm:pr-4">
                        <Button
                          className="flex-1 sm:flex-none"
                          variant="ghost"
                          size="sm"
                          disabled={busy}
                          loading={deny.isPending && deny.variables === j.id}
                          onClick={() => deny.mutate(j.id, { onSuccess: () => toast.success(`Declined ${name}`), onError: (e) => toast.error(errorMessage(e, "Couldn't decline. Try again.")) })}
                        >
                          Decline
                        </Button>
                        <Button
                          className="flex-1 sm:flex-none"
                          size="sm"
                          disabled={busy}
                          loading={approve.isPending && approve.variables === j.id}
                          onClick={() => approve.mutate(j.id, { onSuccess: () => toast.success(`${name} joined`), onError: (e) => toast.error(errorMessage(e, "Couldn't approve. Try again.")) })}
                        >
                          Approve
                        </Button>
                  </span>
                </li>
              );
            })}
          </ul>
        )}
        <LoadMore hasNextPage={requests.hasNextPage} isFetchingNextPage={requests.isFetchingNextPage} fetchNextPage={requests.fetchNextPage} />
      </Card>
    </Section>
  );
}
