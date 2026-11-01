import { errorMessage, hasErrorCode, orgsNoun, orgsNounTitle, useAcceptMyInvite, useActiveOrg, useCreateOrg, useDeclineMyInvite, useJoinableOrgs, useMyInvites, useOrgPickerVisible, useOrgs } from "@scaffoldapp/shared";
import { Avatar, Badge, Button, EmptyState, ErrorState, Field, haptic, Input, ListGroup, ListRow, Screen, Sheet, SkeletonRow, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { Building2, Check, Plus } from "lucide-react-native";
import { useState } from "react";
import { RefreshControl, View } from "react-native";
import { JoinableOrgs } from "./JoinableOrgs";
import { ROLE_LABEL, ROLE_TONE } from "./labels";

function CreateSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const create = useCreateOrg();
  const { setActive } = useActiveOrg();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const submit = async () => {
    const n = name.trim();
    if (!n) return setError("Enter a name.");
    setError(null);
    try {
      const org = await create.mutateAsync({ name: n });
      haptic.success();
      setActive(org.id);
      setName("");
      onClose();
      toast.success(`${org.name} created`);
      router.push({ pathname: "/settings/orgs/[orgID]", params: { orgID: org.id } });
    } catch (e) {
      if (hasErrorCode(e, "invalid_request")) setError(errorMessage(e));
      else toast.error(errorMessage(e, `Couldn't create the ${orgsNoun()}. Try again.`));
    }
  };
  return (
    <Sheet
      open={open}
      onClose={() => !create.isPending && onClose()}
      title={`New ${orgsNoun()}`}
      snapPoints={[300]}
      footer={
        <Button fullWidth loading={create.isPending} onPress={() => void submit()}>
          Create
        </Button>
      }
    >
      <View style={{ gap: theme.space[4] }}>
        <Field label="Name" error={error}>
          <Input value={name} onChangeText={setName} maxLength={80} autoFocus returnKeyType="done" onSubmitEditing={() => void submit()} />
        </Field>
      </View>
    </Sheet>
  );
}

/** Settings → Organizations: invitations to you, orgs your email domain lets you join, the active org, every org you belong to. */
export function OrgsScreen() {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const orgs = useOrgs();
  const invites = useMyInvites();
  const joinable = useJoinableOrgs();
  const accept = useAcceptMyInvite();
  const decline = useDeclineMyInvite();
  const { org: active, setActive } = useActiveOrg();
  const picker = useOrgPickerVisible();
  const [creating, setCreating] = useState(false);
  const rows = orgs.data?.items ?? [];
  const pending = invites.data?.items ?? [];

  return (
    <Screen
      edges={["bottom"]}
      gap={s[8]}
      refreshControl={
        <RefreshControl
          refreshing={orgs.isRefetching && !orgs.isFetchingNextPage}
          onRefresh={() => {
            void orgs.refetch();
            void invites.refetch();
            void joinable.refetch();
          }}
        />
      }
      footer={
        rows.length > 0 ? (
          <Button fullWidth variant="secondary" leftIcon={Plus} onPress={() => setCreating(true)}>
            New {orgsNoun()}
          </Button>
        ) : undefined
      }
    >
      <Stack.Screen options={{ title: orgsNounTitle("many") }} />

      {pending.length > 0 ? (
        <ListGroup title="Invitations">
          {pending.map((inv) => (
            <View key={inv.id} style={{ gap: s[2], paddingBottom: s[3] }}>
              <ListRow
                title={inv.org.name}
                info={`${inv.invited_by_name ? `${inv.invited_by_name} invited you` : "You were invited"} as ${inv.role === "admin" ? "an admin" : "a member"}.`}
                leading={<Avatar name={inv.org.name} src={inv.org.avatar_url} size="md" />}
              />
              <View style={{ flexDirection: "row", gap: s[2], paddingHorizontal: s[4] }}>
                <Button
                  variant="ghost"
                  size="md"
                  style={{ flex: 1 }}
                  disabled={accept.isPending}
                  onPress={() => decline.mutate(inv.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't decline. Try again.")) })}
                >
                  Decline
                </Button>
                <Button
                  size="md"
                  style={{ flex: 1 }}
                  loading={accept.isPending && accept.variables === inv.id}
                  onPress={() =>
                    accept.mutate(inv.id, {
                      onSuccess: (org) => {
                        haptic.success();
                        setActive(org.id);
                        toast.success(`You joined ${org.name}`);
                      },
                      onError: (e) => toast.error(errorMessage(e, "Couldn't accept. Try again.")),
                    })
                  }
                >
                  Join
                </Button>
              </View>
            </View>
          ))}
        </ListGroup>
      ) : null}

      <JoinableOrgs />

      {rows.length > 1 && picker ? (
        <ListGroup title={`Active ${orgsNoun()}`} info={`Screens that work per ${orgsNoun()} show this one.`}>
          {rows.map((o) => (
            <ListRow
              key={o.id}
              title={o.name}
              chevron={false}
              trailing={active?.id === o.id ? <Check size={20} color={c.primary} strokeWidth={2.5} /> : undefined}
              onPress={() => {
                haptic.selection();
                setActive(o.id);
              }}
            />
          ))}
        </ListGroup>
      ) : null}

      <ListGroup title={`Your ${orgsNoun("many")}`}>
        {orgs.isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
            <SkeletonRow />
          </View>
        ) : orgs.isError ? (
          <ErrorState compact title={`Couldn't load your ${orgsNoun("many")}`} info={errorMessage(orgs.error)} onRetry={() => void orgs.refetch()} retrying={orgs.isRefetching} />
        ) : rows.length === 0 ? (
          <EmptyState
            compact
            icon={Building2}
            title={`No ${orgsNoun("many")} yet`}
            action={
              <Button leftIcon={Plus} onPress={() => setCreating(true)}>
                New {orgsNoun()}
              </Button>
            }
          />
        ) : (
          rows.map((o) => (
            <ListRow
              key={o.id}
              title={o.name}
              leading={<Avatar name={o.name} src={o.avatar_url} size="md" />}
              trailing={
                <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
                  {!picker && rows.length > 1 && active?.id === o.id ? <Badge tone="success">Active</Badge> : null}
                  <Badge tone={ROLE_TONE[o.role]}>{ROLE_LABEL[o.role]}</Badge>
                </View>
              }
              onPress={() => router.push({ pathname: "/settings/orgs/[orgID]", params: { orgID: o.id } })}
            />
          ))
        )}
      </ListGroup>
      <CreateSheet open={creating} onClose={() => setCreating(false)} />
    </Screen>
  );
}
