import {
  errorMessage,
  orgsNoun,
  orgsNounA,
  orgsNounTitle,
  topic,
  useAccessAddGrants,
  useAccessCreateLink,
  useAccessRemoveGrant,
  useAccessResource,
  useAccessRevokeLink,
  useAccessSetGeneral,
  useAccessSetInherit,
  useAccessTransferOwner,
  useAccessUpdateGrant,
  useOrg,
  useTopic,
  type AccessDirectoryEntry,
  type AccessGrant,
  type AccessLevel,
  type AccessSharing,
} from "@scaffoldapp/shared";
import {
  Avatar,
  Badge,
  Banner,
  Button,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  haptic,
  ListGroup,
  ListRow,
  SegmentedControl,
  Sheet,
  SkeletonRow,
  Switch,
  Text,
  toast,
  useTheme,
} from "@scaffoldapp/ui-native";
import { Building2, Crown, Globe, Link2, Lock, UserMinus, X } from "lucide-react-native";
import { useMemo, useState } from "react";
import { Share, View } from "react-native";
import { LEVEL_LABEL, LEVEL_SHORT, memberCount } from "./labels";
import { PeoplePicker } from "./PeoplePicker";

export interface ShareSheetProps {
  /** Registered access kind ("docs.page"). */
  kind: string;
  id: string;
  /** Heading until the API's title loads. */
  title?: string;
  open: boolean;
  onClose: () => void;
}

/**
 * Share one resource on mobile: people and teams with levels, general access, the parent
 * it inherits from and public links (shared through the system share sheet). People who
 * can only view see how it is shared. Same rules and API as the web ShareDialog.
 */
export function ShareSheet({ kind, id, title, open, onClose }: ShareSheetProps) {
  const sharing = useAccessResource(kind, open ? id : null);
  useTopic(open && sharing.data ? topic("access", id) : null);
  const name = title ?? sharing.data?.title ?? "";
  const data = sharing.data;
  return (
    <Sheet open={open} onClose={onClose} title={name ? `Share “${name}”` : "Share"} snapPoints={["85%"]}>
      {sharing.isPending ? (
        <View accessibilityLabel="Loading" accessibilityRole="progressbar">
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </View>
      ) : sharing.isError ? (
        sharing.error.status === 404 ? (
          <EmptyState compact icon={Lock} title="This isn't shared with you" />
        ) : (
          <ErrorState compact title="Couldn't load sharing" info={errorMessage(sharing.error)} onRetry={() => void sharing.refetch()} retrying={sharing.isRefetching} />
        )
      ) : data ? (
        data.can_manage ? (
          <Manage data={data} />
        ) : (
          <ReadOnly data={data} />
        )
      ) : null}
    </Sheet>
  );
}

function sourceText(prefix: string, s: NonNullable<AccessSharing["parent"]>): string {
  return s.title ? `${prefix} “${s.title}”` : `${prefix} a ${s.label} you can't open`;
}

function generalText(data: AccessSharing): string {
  const g = data.general;
  return g.access === "org" && g.level ? `Everyone in the ${orgsNoun()}: ${LEVEL_LABEL[g.level].toLowerCase()}` : "Only people with access";
}

function ReadOnly({ data }: { data: AccessSharing }) {
  const theme = useTheme();
  return (
    <View style={{ gap: theme.space[6] }}>
      <ListGroup>
        <ListRow title="Your access" trailing={<Badge tone="primary">{LEVEL_LABEL[data.my_level]}</Badge>} info="Only people who manage it change who has access." />
        {data.owner ? <ListRow title={data.owner.name} leading={<Avatar name={data.owner.name} src={data.owner.avatar_url} size="sm" />} trailing={<Badge>Owner</Badge>} /> : null}
        <ListRow title={generalText(data)} leading={data.general.access === "org" ? <Building2 size={20} color={theme.colors.textMuted} /> : <Lock size={20} color={theme.colors.textMuted} />} {...(data.general.inherited_from ? { info: sourceText("From", data.general.inherited_from) } : {})} />
        {data.parent && data.inherits ? <ListRow title={sourceText("Access comes from", data.parent)} /> : null}
      </ListGroup>
    </View>
  );
}

const rank = (l: AccessLevel) => ["view", "comment", "edit", "manage"].indexOf(l);

function Manage({ data }: { data: AccessSharing }) {
  const theme = useTheme();
  const direct = data.grants.filter((g) => !g.inherited_from);
  const inherited = data.grants.filter((g) => {
    if (!g.inherited_from) return false;
    const own = direct.find((d) => d.subject.id === g.subject.id);
    return !own || rank(own.level) < rank(g.level);
  });
  const [selected, setSelected] = useState<string | null>(null);
  return (
    <View style={{ gap: theme.space[8] }}>
      {data.admin_override ? <Banner tone="info" title={`You manage this as ${orgsNounA()} admin. Changes are recorded.`} /> : null}
      <AddPeople data={data} exclude={new Set(direct.map((g) => g.subject.id))} />
      <ListGroup title="People with access">
        {data.owner ? (
          <ListRow title={data.is_owner ? `${data.owner.name} (you)` : data.owner.name} leading={<Avatar name={data.owner.name} src={data.owner.avatar_url} size="sm" />} trailing={<Badge>Owner</Badge>} />
        ) : (
          <ListRow title="No owner" info={`Its owner left or deleted their account. ${orgsNounTitle()} admins manage it; make someone the owner below.`} />
        )}
        {direct.map((g) =>
          selected === g.id ? (
            <GrantEditor key={g.id} data={data} grant={g} onDone={() => setSelected(null)} />
          ) : (
            <ListRow
              key={g.id}
              title={g.subject.type === "team" ? `${g.subject.name} (@${g.subject.handle})` : g.subject.name}
              leading={<Avatar name={g.subject.name} src={g.subject.avatar_url} size="sm" />}
              trailing={<Text tone="muted">{LEVEL_LABEL[g.level]}</Text>}
              accessibilityHint="Change their access"
              onPress={() => setSelected(g.id)}
            />
          ),
        )}
        {inherited.map((g) => (
          <ListRow
            key={g.id}
            title={g.subject.name}
            leading={<Avatar name={g.subject.name} src={g.subject.avatar_url} size="sm" />}
            trailing={<Text tone="muted">{LEVEL_LABEL[g.level]}</Text>}
            info={g.inherited_from ? `${sourceText("From", g.inherited_from)}. Change it there.` : undefined}
          />
        ))}
      </ListGroup>
      <General data={data} />
      {data.parent ? <Inherit data={data} /> : null}
      {data.public_links.supported ? <PublicLinks data={data} /> : null}
    </View>
  );
}

function AddPeople({ data, exclude }: { data: AccessSharing; exclude: ReadonlySet<string> }) {
  const theme = useTheme();
  const add = useAccessAddGrants(data.kind, data.id);
  const [picked, setPicked] = useState<AccessDirectoryEntry[]>([]);
  const [level, setLevel] = useState<AccessLevel>(data.levels.includes("edit") ? "edit" : (data.levels[0] ?? "view"));
  const [notify, setNotify] = useState(true);
  const hidden = useMemo(() => new Set([...exclude, ...picked.map((p) => p.id), ...(data.owner ? [data.owner.id] : [])]), [exclude, picked, data.owner]);
  if (!data.org_id) return <Banner tone="info" title="Personal items can only be shared from the app that made them." />;
  return (
    <View style={{ gap: theme.space[3] }}>
      <PeoplePicker orgId={data.org_id} exclude={hidden} onPick={(e) => setPicked((p) => [...p, e])} />
      {picked.length > 0 ? (
        <View style={{ gap: theme.space[3] }}>
          <ListGroup>
            {picked.map((p) => (
              <ListRow
                key={p.id}
                title={p.type === "team" ? `@${p.handle}` : p.name}
                leading={<Avatar name={p.name} src={p.avatar_url} size="sm" />}
                trailing={<X size={20} color={theme.colors.textMuted} />}
                chevron={false}
                accessibilityHint="Remove from the list"
                onPress={() => setPicked((all) => all.filter((x) => x.id !== p.id))}
              />
            ))}
          </ListGroup>
          <SegmentedControl<AccessLevel> accessibilityLabel="Access level" value={level} onValueChange={setLevel} options={data.levels.map((l) => ({ value: l, label: LEVEL_SHORT[l] }))} />
          <Switch value={notify} onValueChange={setNotify} label="Notify them" />
          <Button
            loading={add.isPending}
            onPress={() =>
              add.mutate(
                { subjects: picked.map((p) => ({ type: p.type, id: p.id })), level, notify },
                {
                  onSuccess: () => {
                    haptic.success();
                    toast.success(picked.length === 1 ? `Shared with ${picked[0]?.name}` : `Shared with ${picked.length}`);
                    setPicked([]);
                  },
                  onError: (e) => toast.error(errorMessage(e, "Couldn't share. Try again.")),
                },
              )
            }
          >
            Share
          </Button>
        </View>
      ) : null}
    </View>
  );
}

function GrantEditor({ data, grant: g, onDone }: { data: AccessSharing; grant: AccessGrant; onDone: () => void }) {
  const theme = useTheme();
  const update = useAccessUpdateGrant(data.kind, data.id);
  const remove = useAccessRemoveGrant(data.kind, data.id);
  const transfer = useAccessTransferOwner(data.kind, data.id);
  const [confirmOwner, setConfirmOwner] = useState(false);
  const canMakeOwner = g.subject.type === "user" && (data.is_owner || data.admin_override || data.owner === null);
  return (
    <View style={{ gap: theme.space[3], paddingVertical: theme.space[3], paddingHorizontal: theme.space[4] }}>
      <Text weight={600}>{g.subject.type === "team" ? `${g.subject.name} · ${memberCount(g.subject.member_count)}` : g.subject.name}</Text>
      <SegmentedControl<AccessLevel>
        accessibilityLabel={`Access for ${g.subject.name}`}
        value={g.level}
        onValueChange={(l) => update.mutate({ grantId: g.id, level: l }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change access. Try again.")) })}
        options={data.levels.map((l) => ({ value: l, label: LEVEL_SHORT[l] }))}
      />
      <View style={{ flexDirection: "row", flexWrap: "wrap", gap: theme.space[2] }}>
        {canMakeOwner ? (
          <Button variant="secondary" size="md" leftIcon={Crown} onPress={() => setConfirmOwner(true)}>
            Make owner
          </Button>
        ) : null}
        <Button
          variant="danger"
          size="md"
          leftIcon={UserMinus}
          loading={remove.isPending}
          onPress={() =>
            remove.mutate(g.id, {
              onSuccess: () => {
                toast.success(`${g.subject.name} removed`);
                onDone();
              },
              onError: (e) => toast.error(errorMessage(e, "Couldn't remove access. Try again.")),
            })
          }
        >
          Remove
        </Button>
        <Button variant="ghost" size="md" onPress={onDone}>
          Done
        </Button>
      </View>
      <ConfirmDialog
        open={confirmOwner}
        onClose={() => setConfirmOwner(false)}
        title={`Make ${g.subject.name} the owner?`}
        confirmLabel="Make owner"
        errorText={(e) => errorMessage(e, "Couldn't change the owner. Try again.")}
        onConfirm={async () => {
          await transfer.mutateAsync(g.subject.id);
          toast.success(`${g.subject.name} is the owner`);
          onDone();
        }}
      >
        <Text tone="muted">{data.owner ? "They get full control. The current owner keeps manage access." : "They get full control."}</Text>
      </ConfirmDialog>
    </View>
  );
}

function General({ data }: { data: AccessSharing }) {
  const theme = useTheme();
  const set = useAccessSetGeneral(data.kind, data.id);
  const org = useOrg(data.org_id);
  if (!data.org_id) return null;
  const own = data.general.own_level;
  const orgLevels = data.levels.filter((l): l is Exclude<AccessLevel, "manage"> => l !== "manage");
  const save = (access: "restricted" | "org", level?: Exclude<AccessLevel, "manage">) =>
    set.mutate(access === "org" ? { access, level: level ?? "view" } : { access }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change general access. Try again.")) });
  return (
    <ListGroup title="General access" info={`Restricted: only the people and teams listed. ${orgsNounTitle()}: every member gets this level too.`}>
      <View style={{ gap: theme.space[3], paddingVertical: theme.space[3], paddingHorizontal: theme.space[4] }}>
        <SegmentedControl<"restricted" | "org">
          accessibilityLabel="Who can open it"
          value={own ? "org" : "restricted"}
          onValueChange={(v) => save(v, own && own !== "manage" ? own : orgLevels[0])}
          options={[
            { value: "restricted", label: "Restricted" },
            { value: "org", label: org.data?.name ?? orgsNounTitle() },
          ]}
        />
        {own && own !== "manage" ? (
          <SegmentedControl<Exclude<AccessLevel, "manage">> accessibilityLabel="Their level" value={own} onValueChange={(l) => save("org", l)} options={orgLevels.map((l) => ({ value: l, label: LEVEL_SHORT[l] }))} />
        ) : null}
        {data.general.inherited_from && data.general.level ? <Text tone="muted">{`${generalText(data)} · ${sourceText("from", data.general.inherited_from)}`}</Text> : null}
      </View>
    </ListGroup>
  );
}

function Inherit({ data }: { data: AccessSharing }) {
  const set = useAccessSetInherit(data.kind, data.id);
  const [confirm, setConfirm] = useState(false);
  const parent = data.parent;
  if (!parent) return null;
  return (
    <ListGroup title="Parent" info={`People with access to the parent ${parent.label} can open this too, unless you restrict it.`}>
      <ListRow title={sourceText(data.inherits ? "Inherits from" : "Doesn't inherit from", parent)} />
      <ListRow
        title="Only people added here"
        trailing={
          <Switch
            value={!data.inherits}
            disabled={set.isPending}
            accessibilityLabel="Only people added here"
            onValueChange={(restrict) => (restrict ? setConfirm(true) : set.mutate(true, { onError: (e) => toast.error(errorMessage(e)) }))}
          />
        }
      />
      <ConfirmDialog
        open={confirm}
        onClose={() => setConfirm(false)}
        title="Stop inheriting access?"
        tone="danger"
        confirmLabel="Restrict"
        errorText={(e) => errorMessage(e, "Couldn't restrict it. Try again.")}
        onConfirm={async () => {
          await set.mutateAsync(false);
          toast.success("Only people added here can open it");
        }}
      >
        <Text tone="muted">People who can open it only through the parent lose access right away. You keep manage access.</Text>
      </ConfirmDialog>
    </ListGroup>
  );
}

function PublicLinks({ data }: { data: AccessSharing }) {
  const theme = useTheme();
  const create = useAccessCreateLink(data.kind, data.id);
  const revoke = useAccessRevokeLink(data.kind, data.id);
  const pl = data.public_links;
  const share = async () => {
    try {
      const link = await create.mutateAsync({ level: "view", expires_at: new Date(Date.now() + 7 * 86_400_000).toISOString() });
      if (link.url) await Share.share({ message: link.url });
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't make the link. Try again."));
    }
  };
  return (
    <ListGroup title="Public link" info="Anyone with the link can open it without an account. New links last 7 days; revoke one when you're done.">
      {pl.inherited ? <ListRow title={`${sourceText("A public link on", pl.inherited)} also opens this`} leading={<Globe size={20} color={theme.colors.textMuted} />} /> : null}
      {data.can_create_link ? (
        <ListRow title="Share a new link" leading={<Link2 size={20} color={theme.colors.primary} />} chevron={false} disabled={create.isPending} onPress={() => void share()} />
      ) : (
        <ListRow
          title="You can't make public links here"
          leading={<Lock size={20} color={theme.colors.textMuted} />}
          info={`Ask an admin for the “${pl.missing_permission?.title ?? "Share with a public link"}” permission.`}
        />
      )}
      {data.links.map((l) => (
        <ListRow
          key={l.id}
          title={`${LEVEL_LABEL[l.level]} · ${l.expires_at ? `until ${new Date(l.expires_at).toLocaleDateString()}` : "no expiry"}`}
          leading={<Globe size={20} color={theme.colors.textMuted} />}
          {...(!l.working ? { info: "Paused: whoever made it no longer has permission to share it publicly." } : {})}
          trailing={
            <Button
              variant="ghost"
              size="md"
              loading={revoke.isPending && revoke.variables === l.id}
              onPress={() => revoke.mutate(l.id, { onSuccess: () => toast.success("Link revoked"), onError: (e) => toast.error(errorMessage(e)) })}
            >
              Revoke
            </Button>
          }
        />
      ))}
    </ListGroup>
  );
}
