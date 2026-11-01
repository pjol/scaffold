import {
  errorMessage,
  orgRoleAtLeast,
  ORG_AVATAR_PURPOSE,
  topic,
  useCreateInvite,
  useCreateLink,
  useDeleteOrg,
  useMe,
  useOrg,
  useOrgInvites,
  useOrgLinks,
  useOrgMembers,
  useRemoveOrgMember,
  useResendInvite,
  useRevokeInvite,
  useRevokeLink,
  useTopic,
  useTransferOwnership,
  useUpdateMember,
  useUpdateOrg,
  useUpload,
  type Org,
  type OrgInvite,
  type OrgMember,
  type OrgRole,
} from "@scaffoldapp/shared";
import {
  Avatar,
  Badge,
  Button,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  Field,
  haptic,
  Input,
  ListGroup,
  ListRow,
  pickPhotos,
  Screen,
  SegmentedControl,
  Sheet,
  Skeleton,
  SkeletonRow,
  Text,
  Title,
  toast,
  useTheme,
} from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams } from "expo-router";
import { Building2, Crown, Images, Link2, LogOut, Mail, RotateCw, Send, Share2, ShieldCheck, Trash2, UserMinus, UserRound, Users, X } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Share, View } from "react-native";
import { ROLE_LABEL, ROLE_TONE } from "./labels";

type TabId = "general" | "members" | "invites";

function Loading() {
  return (
    <View accessibilityLabel="Loading" accessibilityRole="progressbar">
      <SkeletonRow />
      <SkeletonRow />
    </View>
  );
}

// ---- general -----------------------------------------------------------------

function General({ org }: { org: Org }) {
  const theme = useTheme();
  const s = theme.space;
  const me = useMe();
  const update = useUpdateOrg(org.id);
  const upload = useUpload();
  const leave = useRemoveOrgMember(org.id);
  const del = useDeleteOrg();
  const [name, setName] = useState(org.name);
  const [confirm, setConfirm] = useState<"leave" | "delete" | null>(null);
  const [typed, setTyped] = useState("");
  const admin = orgRoleAtLeast(org.role, "admin");
  useEffect(() => setName(org.name), [org.name]);
  const dirty = name.trim() !== org.name && name.trim().length > 0;

  const changePicture = async () => {
    try {
      const [photo] = await pickPhotos("library", 1, 0.8);
      if (!photo) return;
      const r = await upload.mutateAsync({ file: photo, purpose: ORG_AVATAR_PURPOSE, maxBytes: 5 * 1024 * 1024 });
      await update.mutateAsync({ avatar_key: r.key });
      haptic.success();
      toast.success("Picture updated");
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't update the picture. Try again."));
    }
  };

  return (
    <View style={{ gap: s[8] }}>
      {admin ? (
        <View style={{ gap: s[4] }}>
          <Field label="Name">
            <Input value={name} onChangeText={setName} maxLength={80} returnKeyType="done" />
          </Field>
          <Button
            disabled={!dirty}
            loading={update.isPending && update.variables?.name !== undefined}
            onPress={() =>
              update.mutate(
                { name: name.trim() },
                { onSuccess: () => toast.success("Saved"), onError: (e) => toast.error(errorMessage(e, "Couldn't save. Try again.")) },
              )
            }
          >
            Save
          </Button>
          <Button variant="secondary" leftIcon={Images} loading={upload.isPending} onPress={() => void changePicture()}>
            {org.avatar_url ? "Change picture" : "Add picture"}
          </Button>
        </View>
      ) : null}

      <ListGroup>
        <ListRow title="Leave organization" destructive chevron={false} leading={<LogOut size={22} color={theme.colors.dangerStrong} strokeWidth={2} />} onPress={() => setConfirm("leave")} />
        {org.role === "owner" ? (
          <ListRow title="Delete organization" destructive chevron={false} leading={<Trash2 size={22} color={theme.colors.dangerStrong} strokeWidth={2} />} onPress={() => setConfirm("delete")} />
        ) : null}
      </ListGroup>

      <ConfirmDialog
        open={confirm === "leave"}
        onClose={() => setConfirm(null)}
        title={`Leave ${org.name}?`}
        tone="danger"
        confirmLabel="Leave"
        errorText={(e) => errorMessage(e, "Couldn't leave. Try again.")}
        onConfirm={async () => {
          if (!me.data) return;
          await leave.mutateAsync({ userId: me.data.id, leave: true });
          toast.success(`You left ${org.name}`);
          router.back();
        }}
      >
        <Text tone="muted">You lose access to its content. Someone has to invite you again to rejoin.</Text>
      </ConfirmDialog>

      <ConfirmDialog
        open={confirm === "delete"}
        onClose={() => {
          setConfirm(null);
          setTyped("");
        }}
        title={`Delete ${org.name}?`}
        tone="danger"
        confirmLabel="Delete"
        confirmDisabled={typed.trim() !== "DELETE"}
        errorText={(e) => errorMessage(e, "Couldn't delete the organization. Try again.")}
        onConfirm={async () => {
          await del.mutateAsync(org.id);
          toast.success(`${org.name} deleted`);
          router.back();
        }}
      >
        <View style={{ gap: s[3] }}>
          <Text tone="muted">{`All ${org.member_count} members lose access, and invites and links stop working. It can't be undone.`}</Text>
          <Field label="Type DELETE to confirm">
            <Input value={typed} onChangeText={setTyped} autoCapitalize="characters" autoCorrect={false} />
          </Field>
        </View>
      </ConfirmDialog>
    </View>
  );
}

// ---- members -----------------------------------------------------------------

type MemberAction = { kind: "role"; role: OrgRole } | { kind: "transfer" } | { kind: "remove" };

function actionsFor(viewer: OrgRole, m: OrgMember): MemberAction[] {
  if (viewer === "owner") {
    return [
      ...(["owner", "admin", "member"] as const).filter((r) => r !== m.role).map((role): MemberAction => ({ kind: "role", role })),
      ...(m.role !== "owner" ? [{ kind: "transfer" } as const] : []),
      { kind: "remove" },
    ];
  }
  if (viewer === "admin" && m.role !== "owner") {
    return m.role === "member" ? [{ kind: "role", role: "admin" }, { kind: "remove" }] : [{ kind: "role", role: "member" }];
  }
  return [];
}

function actionTitle(a: MemberAction): string {
  if (a.kind === "remove") return "Remove";
  if (a.kind === "transfer") return "Transfer ownership";
  return `Make ${ROLE_LABEL[a.role].toLowerCase()}`;
}

function Members({ org }: { org: Org }) {
  const theme = useTheme();
  const c = theme.colors;
  const me = useMe();
  const members = useOrgMembers(org.id);
  const update = useUpdateMember(org.id);
  const remove = useRemoveOrgMember(org.id);
  const transfer = useTransferOwnership(org.id);
  const [selected, setSelected] = useState<OrgMember | null>(null);
  const [pending, setPending] = useState<{ member: OrgMember; action: MemberAction } | null>(null);
  const [queued, setQueued] = useState<{ member: OrgMember; action: MemberAction } | null>(null);
  const rows = members.data?.items ?? [];
  const name = (m: OrgMember) => m.display_name || m.email || "Member";
  const icon = (a: MemberAction) => {
    const I = a.kind === "remove" ? UserMinus : a.kind === "transfer" ? Crown : a.role === "owner" ? Crown : a.role === "admin" ? ShieldCheck : UserRound;
    return <I size={22} color={a.kind === "remove" ? c.dangerStrong : c.textMuted} strokeWidth={2} />;
  };

  return (
    <>
      <ListGroup>
        {members.isPending ? (
          <Loading />
        ) : members.isError ? (
          <ErrorState compact title="Couldn't load members" info={errorMessage(members.error)} onRetry={() => void members.refetch()} retrying={members.isRefetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={Users} title="No members" />
        ) : (
          rows.map((m) => {
            const self = m.user_id === me.data?.id;
            const actions = self ? [] : actionsFor(org.role, m);
            return (
              <ListRow
                key={m.user_id}
                title={self ? `${name(m)} (you)` : name(m)}
                leading={<Avatar name={name(m)} src={m.avatar_url} size="md" />}
                trailing={<Badge tone={ROLE_TONE[m.role]}>{ROLE_LABEL[m.role]}</Badge>}
                {...(actions.length > 0 ? { onPress: () => setSelected(m) } : {})}
              />
            );
          })
        )}
        {members.hasNextPage ? (
          <Button variant="ghost" loading={members.isFetchingNextPage} onPress={() => void members.fetchNextPage()}>
            Load more
          </Button>
        ) : null}
      </ListGroup>

      <Sheet
        open={selected !== null}
        onClose={() => setSelected(null)}
        onDismissed={() => {
          // Open the confirmation once the sheet's modal has gone.
          if (queued) {
            setPending(queued);
            setQueued(null);
          }
        }}
        title={selected ? name(selected) : ""}
        snapPoints={[360]}
      >
        <View style={{ marginHorizontal: -theme.space[4] }}>
          {selected
            ? actionsFor(org.role, selected).map((a) => (
                <ListRow
                  key={actionTitle(a)}
                  title={actionTitle(a)}
                  destructive={a.kind === "remove"}
                  chevron={false}
                  leading={icon(a)}
                  onPress={() => {
                    setQueued({ member: selected, action: a });
                    setSelected(null);
                  }}
                />
              ))
            : null}
        </View>
      </Sheet>

      <ConfirmDialog
        open={pending !== null}
        onClose={() => setPending(null)}
        title={pending ? `${actionTitle(pending.action)}: ${name(pending.member)}?` : ""}
        tone={pending?.action.kind === "remove" ? "danger" : "primary"}
        confirmLabel={pending ? actionTitle(pending.action) : "Confirm"}
        errorText={(e) => errorMessage(e, "Couldn't change that. Try again.")}
        onConfirm={async () => {
          if (!pending) return;
          const { member, action } = pending;
          if (action.kind === "remove") await remove.mutateAsync({ userId: member.user_id });
          else if (action.kind === "transfer") await transfer.mutateAsync(member.user_id);
          else await update.mutateAsync({ userId: member.user_id, role: action.role });
          haptic.success();
          toast.success(action.kind === "remove" ? `${name(member)} removed` : "Role updated");
        }}
      >
        <Text tone="muted">
          {pending?.action.kind === "remove"
            ? "They lose access right away. You can invite them again later."
            : pending?.action.kind === "transfer"
              ? "They become an owner and you become an admin."
              : "Owners manage everything; admins manage members and invites."}
        </Text>
      </ConfirmDialog>
    </>
  );
}

// ---- invites -----------------------------------------------------------------

function Invites({ org }: { org: Org }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const invites = useOrgInvites(org.id);
  const links = useOrgLinks(org.id);
  const create = useCreateInvite(org.id);
  const resend = useResendInvite(org.id);
  const revoke = useRevokeInvite(org.id);
  const createLink = useCreateLink(org.id);
  const revokeLink = useRevokeLink(org.id);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<"member" | "admin">("member");
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<OrgInvite | null>(null);
  const pending = invites.data?.items ?? [];
  const linkRows = links.data?.items ?? [];

  const send = () => {
    const value = email.trim().toLowerCase();
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)) return setError("Enter a valid email address.");
    setError(null);
    create.mutate(
      { email: value, role },
      {
        onSuccess: () => {
          haptic.success();
          toast.success(`Invite sent to ${value}`);
          setEmail("");
        },
        onError: (e) => setError(errorMessage(e, "Couldn't send the invite. Try again.")),
      },
    );
  };

  const shareLink = async () => {
    try {
      const link = await createLink.mutateAsync({ expires_in_days: 7 });
      if (link.url) await Share.share({ message: link.url });
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't make the link. Try again."));
    }
  };

  return (
    <View style={{ gap: s[8] }}>
      <View style={{ gap: s[4] }}>
        <Field label="Invite by email" info="They get an email with a link that works for 7 days, only for that address." error={error}>
          <Input value={email} onChangeText={setEmail} keyboardType="email-address" autoCapitalize="none" autoCorrect={false} textContentType="emailAddress" placeholder="name@example.com" />
        </Field>
        <SegmentedControl<"member" | "admin">
          accessibilityLabel="Role"
          value={role}
          onValueChange={setRole}
          options={[
            { value: "member", label: "Member" },
            { value: "admin", label: "Admin" },
          ]}
        />
        <Button leftIcon={Send} loading={create.isPending} onPress={send}>
          Send invite
        </Button>
      </View>

      <ListGroup title="Pending invites">
        {invites.isPending ? (
          <Loading />
        ) : invites.isError ? (
          <ErrorState compact title="Couldn't load invites" info={errorMessage(invites.error)} onRetry={() => void invites.refetch()} retrying={invites.isRefetching} />
        ) : pending.length === 0 ? (
          <EmptyState compact icon={Mail} title="No pending invites" />
        ) : (
          pending.map((inv) => (
            <ListRow key={inv.id} title={inv.email} trailing={<Badge tone={inv.role === "admin" ? "info" : "neutral"}>{inv.role === "admin" ? "Admin" : "Member"}</Badge>} onPress={() => setSelected(inv)} />
          ))
        )}
      </ListGroup>

      <ListGroup title="Invite links" info="Anyone signed in with the link joins as a member. Links last 7 days; revoke one when you're done sharing.">
        <ListRow title="Share a new link" leading={<Share2 size={22} color={c.primary} strokeWidth={2} />} chevron={false} disabled={createLink.isPending} onPress={() => void shareLink()} />
        {links.isPending ? (
          <Loading />
        ) : links.isError ? (
          <ErrorState compact title="Couldn't load links" info={errorMessage(links.error)} onRetry={() => void links.refetch()} retrying={links.isRefetching} />
        ) : linkRows.length === 0 ? null : (
          linkRows.map((l) => (
            <ListRow
              key={l.id}
              title={`${l.uses} ${l.uses === 1 ? "use" : "uses"}${l.max_uses ? ` of ${l.max_uses}` : ""}`}
              leading={<Link2 size={22} color={c.textMuted} strokeWidth={2} />}
              trailing={
                l.revoked_at === null && l.active ? (
                  <Button
                    variant="ghost"
                    size="md"
                    loading={revokeLink.isPending && revokeLink.variables === l.id}
                    onPress={() => revokeLink.mutate(l.id, { onSuccess: () => toast.success("Link revoked"), onError: (e) => toast.error(errorMessage(e)) })}
                  >
                    Revoke
                  </Button>
                ) : (
                  <Badge tone="neutral">{l.revoked_at ? "Revoked" : "Ended"}</Badge>
                )
              }
            />
          ))
        )}
      </ListGroup>

      <Sheet open={selected !== null} onClose={() => setSelected(null)} title={selected?.email ?? ""} snapPoints={[260]}>
        <View style={{ marginHorizontal: -s[4] }}>
          <ListRow
            title="Resend"
            chevron={false}
            leading={<RotateCw size={22} color={c.textMuted} strokeWidth={2} />}
            onPress={() => {
              const inv = selected;
              setSelected(null);
              if (inv) resend.mutate(inv.id, { onSuccess: () => toast.success(`Invite sent again to ${inv.email}`), onError: (e) => toast.error(errorMessage(e)) });
            }}
          />
          <ListRow
            title="Revoke"
            destructive
            chevron={false}
            leading={<X size={22} color={c.dangerStrong} strokeWidth={2} />}
            onPress={() => {
              const inv = selected;
              setSelected(null);
              if (inv) revoke.mutate(inv.id, { onSuccess: () => toast.success("Invite revoked"), onError: (e) => toast.error(errorMessage(e)) });
            }}
          />
        </View>
      </Sheet>
    </View>
  );
}

/** Settings → Organizations → one org: General, Members and (admins) Invites. */
export function OrgScreen() {
  const theme = useTheme();
  const s = theme.space;
  const { orgID } = useLocalSearchParams<{ orgID: string }>();
  const org = useOrg(orgID);
  const [tab, setTab] = useState<TabId>("general");
  useTopic(org.data ? topic("orgs", orgID) : null);

  const o = org.data;
  const admin = orgRoleAtLeast(o?.role, "admin");
  const current: TabId = tab === "invites" && !admin ? "general" : tab;

  return (
    <Screen edges={["bottom"]} gap={s[6]} keyboard>
      <Stack.Screen options={{ title: o?.name ?? "Organization" }} />
      {org.isPending ? (
        <View style={{ gap: s[4] }} accessibilityLabel="Loading" accessibilityRole="progressbar">
          <Skeleton shape="circle" width={64} height={64} />
          <Skeleton height={theme.tap.base} />
          <Skeleton height={160} />
        </View>
      ) : org.isError ? (
        org.error.status === 404 ? (
          <EmptyState icon={Building2} title="Organization not found" />
        ) : (
          <ErrorState title="Couldn't load this organization" info={errorMessage(org.error)} onRetry={() => void org.refetch()} retrying={org.isRefetching} />
        )
      ) : o ? (
        <>
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[3] }}>
            <Avatar name={o.name} src={o.avatar_url} size="lg" />
            <Title style={{ flex: 1 }} numberOfLines={2}>
              {o.name}
            </Title>
            <Badge tone={ROLE_TONE[o.role]}>{ROLE_LABEL[o.role]}</Badge>
          </View>
          <SegmentedControl<TabId>
            accessibilityLabel="Organization"
            value={current}
            onValueChange={setTab}
            options={[
              { value: "general", label: "General" },
              { value: "members", label: `Members (${o.member_count})` },
              ...(admin ? [{ value: "invites" as const, label: "Invites" }] : []),
            ]}
          />
          {current === "general" ? <General org={o} /> : current === "members" ? <Members org={o} /> : <Invites org={o} />}
        </>
      ) : null}
    </Screen>
  );
}
