import { ACCESS_PERMISSION_MANAGE_TEAMS, errorMessage, orgsNounA, orgsNounTitle, topic, useAccessCreateTeam, useAccessPermission, useAccessTeams, useActiveOrg, useTopic } from "@scaffoldapp/shared";
import { Avatar, Badge, Button, EmptyState, ErrorState, Field, haptic, Input, ListGroup, ListRow, Screen, SegmentedControl, Sheet, SkeletonRow, Text, TextArea, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { Building2, Plus, Search, Users } from "lucide-react-native";
import { useState } from "react";
import { RefreshControl, View } from "react-native";
import { memberCount, TEAM_ROLE_LABEL } from "./labels";

/** Settings → Teams: the active org's teams (all or yours) and New team for people who manage teams. */
export function TeamsScreen() {
  const theme = useTheme();
  const { org, isPending } = useActiveOrg();
  const [tab, setTab] = useState<"all" | "mine">("all");
  const [q, setQ] = useState("");
  const [creating, setCreating] = useState(false);
  const canCreate = useAccessPermission(org?.id, ACCESS_PERMISSION_MANAGE_TEAMS);
  const teams = useAccessTeams(org?.id, { q, mine: tab === "mine" });
  useTopic(org ? topic("access", org.id) : null);
  const rows = teams.data?.items ?? [];

  if (!isPending && !org) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Teams" }} />
        <EmptyState icon={Building2} title={`Join or create ${orgsNounA()} first`} action={<Button onPress={() => router.push("/settings/orgs")}>{orgsNounTitle("many")}</Button>} />
      </Screen>
    );
  }
  return (
    <Screen
      edges={["bottom"]}
      refreshControl={<RefreshControl refreshing={teams.isRefetching} onRefresh={() => void teams.refetch()} />}
      footer={
        canCreate ? (
          <Button fullWidth leftIcon={Plus} onPress={() => setCreating(true)}>
            New team
          </Button>
        ) : undefined
      }
    >
      <Stack.Screen options={{ title: org ? `Teams · ${org.name}` : "Teams" }} />
      <View style={{ gap: theme.space[3] }}>
        <SegmentedControl<"all" | "mine"> accessibilityLabel="Teams" value={tab} onValueChange={setTab} options={[{ value: "all", label: "All" }, { value: "mine", label: "Yours" }]} />
        <Input value={q} onChangeText={setQ} placeholder="Search teams" leadingIcon={Search} autoCapitalize="none" autoCorrect={false} accessibilityLabel="Search teams" />
      </View>
      <ListGroup>
        {teams.isPending || isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
            <SkeletonRow />
          </View>
        ) : teams.isError ? (
          <ErrorState compact title="Couldn't load teams" info={errorMessage(teams.error)} onRetry={() => void teams.refetch()} retrying={teams.isRefetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={Users} title={q ? `No teams match “${q}”` : tab === "mine" ? "You're not on a team yet" : "No teams yet"} />
        ) : (
          rows.map((t) => (
            <ListRow
              key={t.id}
              title={t.name}
              leading={<Avatar name={t.name} src={t.avatar_url} size="md" />}
              trailing={t.my_role ? <Badge tone={t.my_role === "lead" ? "primary" : "neutral"}>{TEAM_ROLE_LABEL[t.my_role]}</Badge> : <Text role="caption" tone="muted">{memberCount(t.member_count)}</Text>}
              onPress={() => router.push(`/settings/teams/${t.id}`)}
            />
          ))
        )}
        {teams.hasNextPage ? (
          <Button variant="ghost" loading={teams.isFetchingNextPage} onPress={() => void teams.fetchNextPage()}>
            Load more
          </Button>
        ) : null}
      </ListGroup>
      {org ? <CreateTeamSheet orgId={org.id} open={creating} onClose={() => setCreating(false)} /> : null}
    </Screen>
  );
}

function CreateTeamSheet({ orgId, open, onClose }: { orgId: string; open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const create = useAccessCreateTeam(orgId);
  const [name, setName] = useState("");
  const [handle, setHandle] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [goTo, setGoTo] = useState<string | null>(null);
  const submit = () => {
    if (!name.trim()) return setError("Enter a name.");
    setError(null);
    create.mutate(
      { name: name.trim(), ...(handle.trim() ? { handle: handle.trim().replace(/^@/, "").toLowerCase() } : {}), ...(description.trim() ? { description: description.trim() } : {}) },
      {
        onSuccess: (t) => {
          haptic.success();
          toast.success(`@${t.handle} created`);
          setName("");
          setHandle("");
          setDescription("");
          setGoTo(t.id);
          onClose();
        },
        onError: (e) => setError(errorMessage(e, "Couldn't create the team. Try again.")),
      },
    );
  };
  return (
    <Sheet
      open={open}
      onClose={onClose}
      onDismissed={() => {
        if (goTo) router.push(`/settings/teams/${goTo}`);
        setGoTo(null);
      }}
      title="New team"
      snapPoints={["70%"]}
      footer={
        <Button fullWidth loading={create.isPending} onPress={submit}>
          Create
        </Button>
      }
    >
      <View style={{ gap: theme.space[4] }}>
        <Field label="Name" error={error} required>
          <Input value={name} onChangeText={setName} maxLength={60} placeholder="Design" />
        </Field>
        <Field label="Handle" info="People mention the team with @handle. Left empty, one is made from the name.">
          <Input value={handle} onChangeText={(v) => setHandle(v.toLowerCase())} maxLength={33} placeholder="design" autoCapitalize="none" autoCorrect={false} />
        </Field>
        <Field label="Description">
          <TextArea value={description} onChangeText={setDescription} maxLength={280} />
        </Field>
      </View>
    </Sheet>
  );
}
