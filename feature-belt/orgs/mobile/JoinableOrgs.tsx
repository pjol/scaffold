import { errorMessage, hasErrorCode, orgsNounTitle, useActiveOrg, useCancelOrgJoinRequest, useJoinableOrgs, useJoinOrgByDomain, useRequestToJoinOrg, type JoinableOrg, type Org } from "@scaffoldapp/shared";
import { Avatar, Badge, Button, haptic, ListGroup, ListRow, toast, useTheme } from "@scaffoldapp/ui-native";
import { View } from "react-native";

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
  const theme = useTheme();
  const s = theme.space;
  const joinable = useJoinableOrgs();
  const join = useJoinOrgByDomain();
  const request = useRequestToJoinOrg();
  const cancel = useCancelOrgJoinRequest();
  const { setActive } = useActiveOrg();
  const rows = joinable.data?.items ?? [];
  if (joinable.isPending || joinable.isError || rows.length === 0) return null;

  const act = (j: JoinableOrg) => {
    if (j.action === "join") {
      join.mutate(j.org.id, {
        onSuccess: (org) => {
          haptic.success();
          setActive(org.id);
          toast.success(`You joined ${org.name}`);
          onJoined?.(org);
        },
        onError: (e) => {
          toast.error(errorMessage(e, "Couldn't join. Try again."));
          if (hasErrorCode(e, "approval_required")) void joinable.refetch();
        },
      });
    } else if (j.action === "request") {
      request.mutate(j.org.id, {
        onSuccess: () => {
          haptic.success();
          toast.success(`Asked to join ${j.org.name}`);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't send the request. Try again.")),
      });
    } else if (j.request_id) {
      cancel.mutate(j.request_id, { onSuccess: () => toast.success("Request withdrawn"), onError: (e) => toast.error(errorMessage(e, "Couldn't withdraw. Try again.")) });
    }
  };

  return (
    <ListGroup title={`${orgsNounTitle("many")} you can join`} info="Matched by the domain of your verified email address.">
      {rows.map((j) => (
        <View key={j.org.id} style={{ gap: s[2], paddingBottom: s[3] }}>
          <ListRow
            title={j.org.name}
            info={info(j)}
            leading={<Avatar name={j.org.name} src={j.org.avatar_url} size="md" />}
            {...(j.action === "requested" ? { trailing: <Badge tone="info">Requested</Badge> } : {})}
          />
          <View style={{ paddingHorizontal: s[4] }}>
            <Button
              variant={j.action === "join" ? "primary" : j.action === "request" ? "secondary" : "ghost"}
              size="md"
              loading={(join.isPending && join.variables === j.org.id) || (request.isPending && request.variables === j.org.id) || (cancel.isPending && cancel.variables === j.request_id)}
              onPress={() => act(j)}
            >
              {j.action === "join" ? "Join" : j.action === "request" ? "Ask to join" : "Withdraw request"}
            </Button>
          </View>
        </View>
      ))}
    </ListGroup>
  );
}
