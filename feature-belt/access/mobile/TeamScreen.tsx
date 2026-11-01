import {
  errorMessage,
  topic,
  useAccessAddTeamMembers,
  useAccessDeleteTeam,
  useAccessRemoveTeamMember,
  useAccessTeam,
  useAccessTeamMembers,
  useAccessUpdateTeam,
  useAccessUpdateTeamMember,
  useMe,
  useTopic,
  type AccessDirectoryEntry,
  type AccessTeam,
  type AccessTeamMember,
} from "@scaffoldapp/shared";
import { Avatar, Badge, Button, ConfirmDialog, EmptyState, ErrorState, Field, haptic, Input, ListGroup, ListRow, Screen, Sheet, SkeletonRow, Text, TextArea, Title, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams } from "expo-router";
import { Crown, LogOut, Pencil, Trash2, UserMinus, UserPlus, UserRound, Users, X } from "lucide-react-native";
import { useEffect, useMemo, useState } from "react";
import { View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import type { ResourceMenuAction } from "@/features/registry";
import { memberCount, TEAM_ROLE_LABEL } from "./labels";
import { PeoplePicker } from "./PeoplePicker";

/** Settings → Teams → one team: description, members and leads (route param teamID). */
export function TeamScreen() {
  const { teamID } = useLocalSearchParams<{ teamID: string }>();
  const theme = useTheme();
  const me = useMe();
  const team = useAccessTeam(teamID);
  const leave = useAccessRemoveTeamMember(teamID ?? "");
  const del = useAccessDeleteTeam();
  const [editing, setEditing] = useState(false);
  const [adding, setAdding] = useState(false);
  const [confirm, setConfirm] = useState<"leave" | "delete" | null>(null);
  const [typed, setTyped] = useState("");
  useTopic(team.data ? topic("access", team.data.org_id) : null);

  if (team.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Team" }} />
        <View accessibilityLabel="Loading" accessibilityRole="progressbar">
          <SkeletonRow />
          <SkeletonRow />
        </View>
      </Screen>
    );
  }
  if (team.isError) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Team" }} />
        {team.error.status === 404 ? (
          <EmptyState icon={Users} title="Team not found" action={<Button onPress={() => router.back()}>Back</Button>} />
        ) : (
          <ErrorState title="Couldn't load this team" info={errorMessage(team.error)} onRetry={() => void team.refetch()} retrying={team.isRefetching} />
        )}
      </Screen>
    );
  }
  const t = team.data;
  const actions: ResourceMenuAction[] = [
    ...(t.can_manage_members ? [{ id: "add", label: "Add people", icon: UserPlus, onSelect: () => setAdding(true) }] : []),
    ...(t.can_manage_members ? [{ id: "edit", label: t.can_manage ? "Edit details" : "Edit description", icon: Pencil, onSelect: () => setEditing(true) }] : []),
    ...(t.my_role ? [{ id: "leave", label: "Leave team", icon: LogOut, destructive: true, onSelect: () => setConfirm("leave") }] : []),
    ...(t.can_manage ? [{ id: "delete", label: "Delete team", icon: Trash2, destructive: true, onSelect: () => setConfirm("delete") }] : []),
  ];
  return (
    <Screen edges={["bottom"]}>
      <Stack.Screen options={{ title: `@${t.handle}` }} />
      <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[3] }}>
        <Avatar name={t.name} src={t.avatar_url} size="lg" />
        <View style={{ flex: 1 }}>
          <Title>{t.name}</Title>
        </View>
        {t.my_role ? <Badge tone={t.my_role === "lead" ? "primary" : "neutral"}>{TEAM_ROLE_LABEL[t.my_role]}</Badge> : null}
        <ResourceMenu feature="access" title={t.name} actions={actions} label={`Actions for @${t.handle}`} />
      </View>
      {t.description ? <Text>{t.description}</Text> : null}
      <Members team={t} meId={me.data?.id} onAdd={() => setAdding(true)} />

      <EditSheet team={t} open={editing} onClose={() => setEditing(false)} />
      <AddSheet team={t} open={adding} onClose={() => setAdding(false)} />
      <ConfirmDialog
        open={confirm === "leave"}
        onClose={() => setConfirm(null)}
        title={`Leave @${t.handle}?`}
        tone="danger"
        confirmLabel="Leave"
        errorText={(e) => errorMessage(e, "Couldn't leave the team. Try again.")}
        onConfirm={async () => {
          if (!me.data) return;
          await leave.mutateAsync(me.data.id);
          toast.success(`You left @${t.handle}`);
        }}
      >
        <Text tone="muted">You lose what was shared with the team. A lead can add you again.</Text>
      </ConfirmDialog>
      <ConfirmDialog
        open={confirm === "delete"}
        onClose={() => {
          setConfirm(null);
          setTyped("");
        }}
        title={`Delete @${t.handle}?`}
        tone="danger"
        confirmLabel="Delete"
        confirmDisabled={typed.trim() !== t.handle}
        errorText={(e) => errorMessage(e, "Couldn't delete the team. Try again.")}
        onConfirm={async () => {
          await del.mutateAsync({ teamId: t.id, orgId: t.org_id });
          toast.success(`@${t.handle} deleted`);
          router.back();
        }}
      >
        <View style={{ gap: theme.space[3] }}>
          <Text tone="muted">{`Its ${memberCount(t.member_count)} lose everything shared with the team. It can't be undone.`}</Text>
          <Field label={`Type ${t.handle} to confirm`}>
            <Input value={typed} onChangeText={setTyped} autoCapitalize="none" autoCorrect={false} />
          </Field>
        </View>
      </ConfirmDialog>
    </Screen>
  );
}

function Members({ team: t, meId, onAdd }: { team: AccessTeam; meId: string | undefined; onAdd: () => void }) {
  const members = useAccessTeamMembers(t.id);
  const update = useAccessUpdateTeamMember(t.id);
  const remove = useAccessRemoveTeamMember(t.id);
  const [removing, setRemoving] = useState<AccessTeamMember | null>(null);
  const rows = members.data?.items ?? [];
  return (
    <ListGroup title={`Members · ${t.member_count}`} info="Leads add and remove members. People who manage teams can change any team.">
      {members.isPending ? (
        <SkeletonRow />
      ) : members.isError ? (
        <ErrorState compact title="Couldn't load members" info={errorMessage(members.error)} onRetry={() => void members.refetch()} retrying={members.isRefetching} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={Users} title="No members yet" {...(t.can_manage_members ? { action: <Button onPress={onAdd}>Add people</Button> } : {})} />
      ) : (
        rows.map((m) => {
          const self = m.user_id === meId;
          const actions: ResourceMenuAction[] =
            t.can_manage_members && !self
              ? [
                  m.role === "lead"
                    ? { id: "member", label: "Make member", icon: UserRound, onSelect: () => update.mutate({ userId: m.user_id, role: "member" }, { onError: (e) => toast.error(errorMessage(e)) }) }
                    : { id: "lead", label: "Make lead", icon: Crown, onSelect: () => update.mutate({ userId: m.user_id, role: "lead" }, { onError: (e) => toast.error(errorMessage(e)) }) },
                  { id: "remove", label: "Remove from team", icon: UserMinus, destructive: true, onSelect: () => setRemoving(m) },
                ]
              : [];
          return (
            <ListRow
              key={m.user_id}
              title={self ? `${m.display_name} (you)` : m.display_name}
              leading={<Avatar name={m.display_name || "Member"} src={m.avatar_url} size="md" />}
              trailing={
                <View style={{ flexDirection: "row", alignItems: "center" }}>
                  <Badge tone={m.role === "lead" ? "primary" : "neutral"}>{TEAM_ROLE_LABEL[m.role]}</Badge>
                  {actions.length > 0 ? <ResourceMenu feature="access" title={m.display_name} actions={actions} label={`Actions for ${m.display_name}`} /> : null}
                </View>
              }
            />
          );
        })
      )}
      {members.hasNextPage ? (
        <Button variant="ghost" loading={members.isFetchingNextPage} onPress={() => void members.fetchNextPage()}>
          Load more
        </Button>
      ) : null}
      <ConfirmDialog
        open={removing !== null}
        onClose={() => setRemoving(null)}
        title={`Remove ${removing?.display_name ?? ""}?`}
        tone="danger"
        confirmLabel="Remove"
        errorText={(e) => errorMessage(e, "Couldn't remove them. Try again.")}
        onConfirm={async () => {
          if (!removing) return;
          await remove.mutateAsync(removing.user_id);
          haptic.success();
          toast.success(`${removing.display_name} removed`);
        }}
      >
        <Text tone="muted">They lose what was shared with the team right away.</Text>
      </ConfirmDialog>
    </ListGroup>
  );
}

function AddSheet({ team: t, open, onClose }: { team: AccessTeam; open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const add = useAccessAddTeamMembers(t.id);
  const members = useAccessTeamMembers(t.id);
  const [picked, setPicked] = useState<AccessDirectoryEntry[]>([]);
  useEffect(() => {
    if (open) setPicked([]);
  }, [open]);
  const hidden = useMemo(() => new Set([...(members.data?.items ?? []).map((m) => m.user_id), ...picked.map((p) => p.id)]), [members.data, picked]);
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={`Add people to @${t.handle}`}
      snapPoints={["85%"]}
      footer={
        <Button
          fullWidth
          disabled={picked.length === 0}
          loading={add.isPending}
          onPress={() =>
            add.mutate(
              { userIds: picked.map((p) => p.id) },
              {
                onSuccess: (r) => {
                  haptic.success();
                  toast.success(r.added === 1 ? "1 person added" : `${r.added} people added`);
                  onClose();
                },
                onError: (e) => toast.error(errorMessage(e, "Couldn't add them. Try again.")),
              },
            )
          }
        >
          {picked.length > 0 ? `Add ${picked.length}` : "Add"}
        </Button>
      }
    >
      <View style={{ gap: theme.space[4] }}>
        <PeoplePicker orgId={t.org_id} types="user" exclude={hidden} onPick={(e) => setPicked((p) => [...p, e])} />
        {picked.length > 0 ? (
          <ListGroup title="Adding">
            {picked.map((p) => (
              <ListRow
                key={p.id}
                title={p.name}
                leading={<Avatar name={p.name} src={p.avatar_url} size="sm" />}
                trailing={<X size={20} color={theme.colors.textMuted} />}
                chevron={false}
                accessibilityHint="Remove from the list"
                onPress={() => setPicked((all) => all.filter((x) => x.id !== p.id))}
              />
            ))}
          </ListGroup>
        ) : null}
      </View>
    </Sheet>
  );
}

function EditSheet({ team: t, open, onClose }: { team: AccessTeam; open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const update = useAccessUpdateTeam(t.id);
  const [name, setName] = useState(t.name);
  const [description, setDescription] = useState(t.description);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!open) return;
    setName(t.name);
    setDescription(t.description);
    setError(null);
  }, [open, t.name, t.description]);
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={t.can_manage ? "Edit team" : "Edit description"}
      snapPoints={["60%"]}
      footer={
        <Button
          fullWidth
          loading={update.isPending}
          onPress={() =>
            update.mutate(
              { ...(t.can_manage && name.trim() !== t.name ? { name: name.trim() } : {}), description: description.trim() },
              { onSuccess: () => (toast.success("Saved"), onClose()), onError: (e) => setError(errorMessage(e, "Couldn't save. Try again.")) },
            )
          }
        >
          Save
        </Button>
      }
    >
      <View style={{ gap: theme.space[4] }}>
        {t.can_manage ? (
          <Field label="Name" error={error}>
            <Input value={name} onChangeText={setName} maxLength={60} />
          </Field>
        ) : null}
        <Field label="Description" {...(!t.can_manage ? { error } : {})}>
          <TextArea value={description} onChangeText={setDescription} maxLength={280} />
        </Field>
      </View>
    </Sheet>
  );
}
