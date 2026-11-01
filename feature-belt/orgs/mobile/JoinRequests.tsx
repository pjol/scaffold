import { errorMessage, useApproveOrgJoinRequest, useDenyOrgJoinRequest, useOrgJoinRequests, type Org } from "@scaffoldapp/shared";
import { Avatar, Button, ErrorState, haptic, ListGroup, ListRow, toast, useTheme } from "@scaffoldapp/ui-native";
import { View } from "react-native";

/**
 * Requests to join (admins), above the member list. Nothing renders while loading or when
 * there are none.
 */
export function JoinRequests({ org }: { org: Org }) {
  const theme = useTheme();
  const s = theme.space;
  const requests = useOrgJoinRequests(org.id);
  const approve = useApproveOrgJoinRequest(org.id);
  const deny = useDenyOrgJoinRequest(org.id);
  const rows = requests.data?.items ?? [];
  if (requests.isPending || (!requests.isError && rows.length === 0)) return null;
  return (
    <ListGroup title="Requests to join" info="People whose verified email is on one of your domains. Declined people can ask again after 7 days.">
      {requests.isError ? (
        <ErrorState compact title="Couldn't load requests" info={errorMessage(requests.error)} onRetry={() => void requests.refetch()} retrying={requests.isRefetching} />
      ) : (
        rows.map((j) => {
          const name = j.display_name || j.email;
          const busy = (approve.isPending && approve.variables === j.id) || (deny.isPending && deny.variables === j.id);
          return (
            <View key={j.id} style={{ gap: s[2], paddingBottom: s[3] }}>
              <ListRow title={name} info={j.email} leading={<Avatar name={name} src={j.avatar_url} size="md" />} />
              <View style={{ flexDirection: "row", gap: s[2], paddingHorizontal: s[4] }}>
                <Button
                  variant="ghost"
                  size="md"
                  style={{ flex: 1 }}
                  disabled={busy}
                  loading={deny.isPending && deny.variables === j.id}
                  onPress={() => deny.mutate(j.id, { onSuccess: () => toast.success(`Declined ${name}`), onError: (e) => toast.error(errorMessage(e, "Couldn't decline. Try again.")) })}
                >
                  Decline
                </Button>
                <Button
                  size="md"
                  style={{ flex: 1 }}
                  disabled={busy}
                  loading={approve.isPending && approve.variables === j.id}
                  onPress={() =>
                    approve.mutate(j.id, {
                      onSuccess: () => {
                        haptic.success();
                        toast.success(`${name} joined`);
                      },
                      onError: (e) => toast.error(errorMessage(e, "Couldn't approve. Try again.")),
                    })
                  }
                >
                  Approve
                </Button>
              </View>
            </View>
          );
        })
      )}
      {requests.hasNextPage ? (
        <Button variant="ghost" loading={requests.isFetchingNextPage} onPress={() => void requests.fetchNextPage()}>
          Load more
        </Button>
      ) : null}
    </ListGroup>
  );
}
