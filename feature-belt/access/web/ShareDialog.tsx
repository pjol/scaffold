"use client";

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
  type AccessLink,
  type AccessSharing,
} from "@scaffoldapp/shared";
import {
  Badge,
  Banner,
  Button,
  Checkbox,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  IconButton,
  InfoTip,
  Input,
  Menu,
  MenuChoice,
  MenuItem,
  MenuSeparator,
  Select,
  SkeletonGroup,
  SkeletonRow,
  Switch,
  toast,
} from "@scaffoldapp/ui-web";
import { Building2, Check, ChevronDown, Copy, Crown, Globe, Link2, Lock, Share2, UserMinus, X } from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { formatDate, formatRelative } from "@/lib/format";
import { LEVEL_LABEL, memberCount } from "./labels";
import { PeoplePicker, SubjectAvatar } from "./PeoplePicker";

export interface ShareDialogProps {
  /** Registered access kind ("docs.page"). */
  kind: string;
  id: string;
  /** The resource's title for the heading; the API's title is used until it loads. */
  title?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * Share one resource: people and teams with levels, general access for the org, the
 * parent it inherits from, and public links. People who can only view see how it is
 * shared and their own level. Live: other people's changes refresh it.
 */
export function ShareDialog({ kind, id, title, open, onOpenChange }: ShareDialogProps) {
  const sharing = useAccessResource(kind, open ? id : null);
  useTopic(open && sharing.data ? topic("access", id) : null);
  const name = title ?? sharing.data?.title ?? "";
  const data = sharing.data;

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={name ? `Share “${name}”` : "Share"}
      size="md"
      footer={
        <>
          {data ? <CopyAppLink path={data.link} /> : null}
          <Button size="auto" onClick={() => onOpenChange(false)}>
            Done
          </Button>
        </>
      }
    >
      {sharing.isPending ? (
        <SkeletonGroup label="Loading sharing" className="flex flex-col gap-2">
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </SkeletonGroup>
      ) : sharing.isError ? (
        sharing.error.status === 404 ? (
          <EmptyState compact icon={<Lock />} title="This isn't shared with you" />
        ) : (
          <ErrorState compact title="Couldn't load sharing" info={sharing.error.message} onRetry={() => void sharing.refetch()} retrying={sharing.isFetching} />
        )
      ) : data ? (
        data.can_manage ? (
          <ManageSharing data={data} />
        ) : (
          <ViewSharing data={data} />
        )
      ) : null}
    </Dialog>
  );
}

/** A Share button that opens the dialog. */
export function ShareButton({ kind, id, title, variant = "secondary" }: { kind: string; id: string; title?: string; variant?: "secondary" | "primary" | "ghost" }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant={variant} size="auto" leftIcon={<Share2 />} onClick={() => setOpen(true)}>
        Share
      </Button>
      <ShareDialog kind={kind} id={id} {...(title ? { title } : {})} open={open} onOpenChange={setOpen} />
    </>
  );
}

function CopyAppLink({ path }: { path: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      variant="ghost"
      size="auto"
      leftIcon={copied ? <Check /> : <Link2 />}
      onClick={() => {
        void copyText(window.location.origin + path).then((ok) => {
          if (!ok) {
            toast.error("Couldn't copy. Copy it from the address bar.");
            return;
          }
          setCopied(true);
          setTimeout(() => setCopied(false), 2000);
        });
      }}
    >
      {copied ? "Copied" : "Copy link"}
    </Button>
  );
}

async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

function Heading({ children, info }: { children: ReactNode; info?: ReactNode }) {
  return (
    <h3 className="flex items-center gap-2 type-label font-semibold text-fg">
      {children}
      {info ? <InfoTip content={info} /> : null}
    </h3>
  );
}

function SourceLink({ source, prefix }: { source: NonNullable<AccessSharing["parent"]>; prefix: string }) {
  if (source.title && source.link) {
    return (
      <>
        {prefix}{" "}
        <Link href={source.link} className="font-semibold text-fg underline underline-offset-2">
          {source.title}
        </Link>
      </>
    );
  }
  return <>{`${prefix} a ${source.label} you can't open`}</>;
}

// ---- read-only view ------------------------------------------------------------

function ViewSharing({ data }: { data: AccessSharing }) {
  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center gap-3">
        <Badge tone="primary">{LEVEL_LABEL[data.my_level]}</Badge>
        <span className="flex items-center gap-2 type-label text-fg-muted">
          Only people who manage it change who has access.
        </span>
      </div>
      {data.owner ? (
        <PersonRow avatar={<SubjectAvatar type="user" name={data.owner.name} src={data.owner.avatar_url} />} name={data.owner.name} trailing={<Badge>Owner</Badge>} />
      ) : null}
      <GeneralSummary data={data} />
      {data.parent && data.inherits ? (
        <p className="type-label text-fg-muted">
          <SourceLink source={data.parent} prefix="Access comes from" />
        </p>
      ) : null}
    </div>
  );
}

function GeneralSummary({ data }: { data: AccessSharing }) {
  const g = data.general;
  return (
    <div className="flex items-center gap-3">
      <span className="flex size-8 shrink-0 items-center justify-center rounded-pill bg-sunken text-fg-muted [&_svg]:size-4">{g.access === "org" ? <Building2 aria-hidden /> : <Lock aria-hidden />}</span>
      <span className="min-w-0 flex-1 type-label text-fg">
        {g.access === "org" && g.level ? `Everyone in the ${orgsNoun()}: ${LEVEL_LABEL[g.level].toLowerCase()}` : "Only people with access"}
        {g.inherited_from ? (
          <span className="text-fg-muted">
            {" · "}
            <SourceLink source={g.inherited_from} prefix="from" />
          </span>
        ) : null}
      </span>
    </div>
  );
}

/** One line: avatar · name (+ muted meta on the same line, dropped first on narrow screens) · control. */
function PersonRow({ avatar, name, detail, trailing }: { avatar: ReactNode; name: string; detail?: ReactNode; trailing?: ReactNode }) {
  return (
    <li className="flex min-h-row items-center gap-3 py-1">
      {avatar}
      <span className="flex min-w-0 flex-1 items-baseline gap-2">
        <span className="min-w-0 shrink truncate type-label text-fg">{name}</span>
        {detail ? <span className="hidden min-w-0 shrink-[2] truncate type-caption text-fg-muted sm:inline">{detail}</span> : null}
      </span>
      {trailing}
    </li>
  );
}

// ---- managing ------------------------------------------------------------------

function ManageSharing({ data }: { data: AccessSharing }) {
  const direct = data.grants.filter((g) => !g.inherited_from);
  const directIds = new Set(direct.map((g) => g.subject.id));
  // Inherited entries someone already has here at the same or a higher level add nothing.
  const inherited = data.grants.filter((g) => {
    if (!g.inherited_from) return false;
    const own = direct.find((d) => d.subject.id === g.subject.id);
    return !own || levelRank(own.level) < levelRank(g.level);
  });
  return (
    <div className="flex flex-col gap-8">
      {data.admin_override ? <Banner tone="info">You manage this as {orgsNounA()} admin. Changes are recorded in the audit log.</Banner> : null}
      <AddPeople data={data} exclude={directIds} />
      <section className="flex flex-col gap-3">
        <Heading>People with access</Heading>
        <ul className="flex flex-col">
          {data.owner ? (
            <PersonRow
              avatar={<SubjectAvatar type="user" name={data.owner.name} src={data.owner.avatar_url} />}
              name={data.is_owner ? `${data.owner.name} (you)` : data.owner.name}
              trailing={<Badge>Owner</Badge>}
            />
          ) : (
            <PersonRow
              avatar={<span className="flex size-8 items-center justify-center rounded-pill bg-sunken text-fg-muted [&_svg]:size-4"><Building2 aria-hidden /></span>}
              name="No owner"
              trailing={<InfoTip content={`Its owner's account was deleted or they left the ${orgsNoun()}. ${orgsNounTitle()} admins manage it; give it a new owner from the list.`} />}
            />
          )}
          {direct.map((g) => (
            <GrantRow key={g.id} data={data} grant={g} />
          ))}
          {inherited.map((g) => (
            <PersonRow
              key={g.id}
              avatar={<SubjectAvatar type={g.subject.type} name={g.subject.name} src={g.subject.avatar_url} />}
              name={g.subject.name}
              detail={g.subject.type === "team" ? `@${g.subject.handle}` : undefined}
              trailing={
                <span className="flex items-center gap-1 pl-3 type-label text-fg-muted">
                  {LEVEL_LABEL[g.level]}
                  <InfoTip content={`From ${g.inherited_from?.title ? `\u201c${g.inherited_from.title}\u201d` : `a parent ${g.inherited_from?.label ?? "item"} you can't open`}. Change it there.`} />
                </span>
              }
            />
          ))}
        </ul>
      </section>
      <GeneralAccess data={data} />
      {data.parent ? <Inheritance data={data} /> : null}
      {data.public_links.supported ? <PublicLinks data={data} /> : null}
    </div>
  );
}

const levelRank = (l: AccessLevel) => ["view", "comment", "edit", "manage"].indexOf(l);

function AddPeople({ data, exclude }: { data: AccessSharing; exclude: ReadonlySet<string> }) {
  const add = useAccessAddGrants(data.kind, data.id);
  const [picked, setPicked] = useState<AccessDirectoryEntry[]>([]);
  const [level, setLevel] = useState<AccessLevel>(data.levels.includes("edit") ? "edit" : (data.levels[0] ?? "view"));
  const [notify, setNotify] = useState(true);
  const hidden = useMemo(() => new Set([...exclude, ...picked.map((p) => p.id), ...(data.owner ? [data.owner.id] : [])]), [exclude, picked, data.owner]);

  if (!data.org_id) {
    return <Banner tone="neutral">Personal items can only be shared from the app that made them.</Banner>;
  }
  const share = () =>
    add.mutate(
      { subjects: picked.map((p) => ({ type: p.type, id: p.id })), level, notify },
      {
        onSuccess: () => {
          toast.success(picked.length === 1 ? `Shared with ${picked[0]?.name}` : `Shared with ${picked.length} people and teams`);
          setPicked([]);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't share. Try again.")),
      },
    );
  return (
    <section className="flex flex-col gap-3">
      <PeoplePicker orgId={data.org_id} exclude={hidden} onPick={(e) => setPicked((p) => [...p, e])} />
      {picked.length > 0 ? (
        <div className="flex flex-col gap-3">
          <ul className="flex flex-wrap gap-2" aria-label="Chosen">
            {picked.map((p) => (
              <li key={p.id} className="flex h-control-sm items-center gap-1.5 rounded-pill bg-sunken pr-1 pl-1 type-label text-fg">
                <SubjectAvatar type={p.type} name={p.name} src={p.avatar_url} size="xs" />
                <span className="max-w-40 truncate">{p.type === "team" ? `@${p.handle}` : p.name}</span>
                <IconButton label={`Remove ${p.name}`} icon={<X />} variant="ghost" size="sm" onClick={() => setPicked((all) => all.filter((x) => x.id !== p.id))} />
              </li>
            ))}
          </ul>
          <div className="flex flex-wrap items-center gap-3">
            <Select
              aria-label="Access level"
              className="w-40"
              value={level}
              onValueChange={(v) => setLevel(v as AccessLevel)}
              options={data.levels.map((l) => ({ value: l, label: LEVEL_LABEL[l] }))}
            />
            <Checkbox checked={notify} onCheckedChange={setNotify} label="Notify" info="Send them a notification with a link." />
            <Button size="auto" className="ml-auto" loading={add.isPending} onClick={share}>
              Share
            </Button>
          </div>
        </div>
      ) : null}
    </section>
  );
}

function GrantRow({ data, grant: g }: { data: AccessSharing; grant: AccessGrant }) {
  const update = useAccessUpdateGrant(data.kind, data.id);
  const remove = useAccessRemoveGrant(data.kind, data.id);
  const transfer = useAccessTransferOwner(data.kind, data.id);
  const [confirmOwner, setConfirmOwner] = useState(false);
  const canMakeOwner = g.subject.type === "user" && (data.is_owner || data.admin_override || data.owner === null);
  const detail = g.subject.type === "team" ? `@${g.subject.handle} · ${memberCount(g.subject.member_count)}` : (g.subject.email ?? undefined);
  return (
    <>
      <PersonRow
        avatar={<SubjectAvatar type={g.subject.type} name={g.subject.name} src={g.subject.avatar_url} />}
        name={g.subject.name}
        detail={detail}
        trailing={
          <Menu
            align="end"
            trigger={
              <Button variant="ghost" size="sm" rightIcon={<ChevronDown />} aria-label={`${g.subject.name}: ${LEVEL_LABEL[g.level]}. Change`}>
                {LEVEL_LABEL[g.level]}
              </Button>
            }
          >
            {data.levels.map((l) => (
              <MenuChoice
                key={l}
                checked={l === g.level}
                {...(l === "manage" ? { hint: "Shares too" } : {})}
                onSelect={() => {
                  if (l === g.level) return;
                  update.mutate({ grantId: g.id, level: l }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change access. Try again.")) });
                }}
              >
                {LEVEL_LABEL[l]}
              </MenuChoice>
            ))}
            {canMakeOwner ? (
              <>
                <MenuSeparator />
                <MenuItem icon={<Crown aria-hidden />} onSelect={() => setConfirmOwner(true)}>
                  Make owner
                </MenuItem>
              </>
            ) : null}
            <MenuSeparator />
            <MenuItem
              destructive
              icon={<UserMinus aria-hidden />}
              onSelect={() =>
                remove.mutate(g.id, {
                  onSuccess: () => toast.success(`${g.subject.name} removed`),
                  onError: (e) => toast.error(errorMessage(e, "Couldn't remove access. Try again.")),
                })
              }
            >
              Remove access
            </MenuItem>
          </Menu>
        }
      />
      <ConfirmDialog
        open={confirmOwner}
        onOpenChange={setConfirmOwner}
        title={`Make ${g.subject.name} the owner?`}
        confirmLabel="Make owner"
        errorText={(e) => errorMessage(e, "Couldn't change the owner. Try again.")}
        onConfirm={async () => {
          await transfer.mutateAsync(g.subject.id);
          toast.success(`${g.subject.name} is the owner`);
        }}
      >
        <p>{data.owner ? `They get full control. ${data.is_owner ? "You" : data.owner.name} keep${data.is_owner ? "" : "s"} manage access.` : "They get full control."}</p>
      </ConfirmDialog>
    </>
  );
}

function GeneralAccess({ data }: { data: AccessSharing }) {
  const set = useAccessSetGeneral(data.kind, data.id);
  const org = useOrg(data.org_id);
  const own = data.general.own_level;
  const orgLevels = data.levels.filter((l): l is Exclude<AccessLevel, "manage"> => l !== "manage");
  if (!data.org_id) return null;
  const orgName = org.data?.name ?? `the ${orgsNoun()}`;
  const save = (access: "restricted" | "org", level?: Exclude<AccessLevel, "manage">) =>
    set.mutate(access === "org" ? { access, level: level ?? "view" } : { access }, {
      onSuccess: () => toast.success(access === "org" ? `Everyone in ${orgName} can ${level ?? "view"}` : "Only people with access can open it"),
      onError: (e) => toast.error(errorMessage(e, "Couldn't change general access. Try again.")),
    });
  return (
    <section className="flex flex-col gap-3">
      <Heading info={`Only people with access: the people and teams listed. Everyone in ${orgName}: every member gets this level too.`}>General access</Heading>
      <div className="flex flex-wrap items-center gap-3">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-pill bg-sunken text-fg-muted [&_svg]:size-4">{own ? <Building2 aria-hidden /> : <Lock aria-hidden />}</span>
        <Select
          aria-label="Who can open it"
          className="min-w-0 flex-1 sm:max-w-64"
          value={own ? "org" : "restricted"}
          disabled={set.isPending}
          onValueChange={(v) => save(v as "restricted" | "org", own && own !== "manage" ? own : orgLevels[0])}
          options={[
            { value: "restricted", label: "Only people with access" },
            { value: "org", label: `Everyone in ${orgName}` },
          ]}
        />
        {own ? (
          <Select
            aria-label="Their level"
            className="w-40"
            value={own}
            disabled={set.isPending}
            onValueChange={(v) => save("org", v as Exclude<AccessLevel, "manage">)}
            options={orgLevels.map((l) => ({ value: l, label: LEVEL_LABEL[l] }))}
          />
        ) : null}
      </div>
      {data.general.inherited_from && data.general.level ? (
        <p className="type-label text-fg-muted">
          {`Everyone in ${orgName} ${LEVEL_LABEL[data.general.level].toLowerCase()} through `}
          <SourceLink source={data.general.inherited_from} prefix="" />
        </p>
      ) : null}
    </section>
  );
}

function Inheritance({ data }: { data: AccessSharing }) {
  const set = useAccessSetInherit(data.kind, data.id);
  const [confirm, setConfirm] = useState(false);
  const parent = data.parent;
  if (!parent) return null;
  return (
    <section className="flex flex-col gap-3">
      <Heading info={`People with access to the parent ${parent.label} can open this too, unless you restrict it.`}>Parent</Heading>
      <p className="type-label text-fg-muted">
        <SourceLink source={parent} prefix={data.inherits ? "Inherits access from" : "Doesn't inherit from"} />
      </p>
      <Switch
        checked={!data.inherits}
        disabled={set.isPending}
        label="Only people added here"
        info="Stop inheriting the parent's access. You keep manage access."
        onCheckedChange={(restrict) => {
          if (restrict) setConfirm(true);
          else set.mutate(true, { onSuccess: () => toast.success("Inherits the parent's access again"), onError: (e) => toast.error(errorMessage(e)) });
        }}
      />
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title="Stop inheriting access?"
        confirmLabel="Restrict"
        tone="danger"
        errorText={(e) => errorMessage(e, "Couldn't restrict it. Try again.")}
        onConfirm={async () => {
          await set.mutateAsync(false);
          toast.success("Only people added here can open it");
        }}
      >
        <p>People who can open it only through the parent lose access right away.</p>
      </ConfirmDialog>
    </section>
  );
}

const EXPIRY = [
  { value: "never", label: "Never expires" },
  { value: "1", label: "Expires in 1 day" },
  { value: "7", label: "Expires in 7 days" },
  { value: "30", label: "Expires in 30 days" },
] as const;

/** Why you can't make a public link here: which org permission is missing. */
function linkRefusal(pl: AccessSharing["public_links"]): string {
  const p = pl.missing_permission;
  if (!p || p.key === "access.share_public") return `Your ${orgsNoun()} doesn't let you make public links. Ask an admin.`;
  return `Public links here need the “${p.title}” permission. Ask an admin.`;
}

function PublicLinks({ data }: { data: AccessSharing }) {
  const create = useAccessCreateLink(data.kind, data.id);
  const revoke = useAccessRevokeLink(data.kind, data.id);
  const pl = data.public_links;
  const [level, setLevel] = useState<"view" | "comment">("view");
  const [expiry, setExpiry] = useState<string>("7");
  const [fresh, setFresh] = useState<AccessLink | null>(null);
  const [copied, setCopied] = useState(false);
  useEffect(() => setCopied(false), [fresh]);

  const make = () =>
    create.mutate(
      { level, ...(expiry === "never" ? {} : { expires_at: new Date(Date.now() + Number(expiry) * 86_400_000).toISOString() }) },
      { onSuccess: (l) => setFresh(l), onError: (e) => toast.error(errorMessage(e, "Couldn't make the link. Try again.")) },
    );

  return (
    <section className="flex flex-col gap-3">
      <Heading info="Anyone with the link can open it without an account. Revoke it when you're done sharing.">Public link</Heading>
      {pl.inherited ? (
        <p className="flex items-center gap-2 type-label text-fg-muted">
          <Globe className="size-4 shrink-0" aria-hidden />
          <span>
            <SourceLink source={pl.inherited} prefix="A public link on" />
            {" also opens this."}
          </span>
        </p>
      ) : null}
      {!data.can_create_link ? (
        <Banner tone="neutral">{linkRefusal(pl)}</Banner>
      ) : (
        <div className="flex flex-wrap items-center gap-3">
          {pl.levels.length > 1 ? (
            <Select
              aria-label="Link access"
              className="w-40"
              value={level}
              onValueChange={(v) => setLevel(v as "view" | "comment")}
              options={pl.levels.map((l) => ({ value: l, label: LEVEL_LABEL[l] }))}
            />
          ) : null}
          <Select aria-label="Expiry" className="w-48" value={expiry} onValueChange={setExpiry} options={EXPIRY.map((e) => ({ value: e.value, label: e.label }))} />
          <Button variant="secondary" size="auto" leftIcon={<Link2 />} loading={create.isPending} onClick={make}>
            Create link
          </Button>
        </div>
      )}
      {fresh?.url ? (
        <div className="flex flex-col gap-2 rounded-md bg-sunken p-3">
          <div className="flex items-center gap-2">
            <Input readOnly value={fresh.url} aria-label="New public link" onFocus={(e) => e.currentTarget.select()} wrapperClassName="min-w-0 flex-1" />
            <Button
              size="auto"
              leftIcon={copied ? <Check /> : <Copy />}
              onClick={() => void copyText(fresh.url ?? "").then((ok) => (ok ? setCopied(true) : toast.error("Couldn't copy. Select the link and copy it.")))}
            >
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
          <p className="type-caption text-fg-muted">Copy it now: for safety it isn&apos;t shown again.</p>
        </div>
      ) : null}
      {data.links.length > 0 ? (
        <ul className="flex flex-col">
          {data.links.map((l) => (
            <PersonRow
              key={l.id}
              avatar={<span className="flex size-8 items-center justify-center rounded-pill bg-sunken text-fg-muted [&_svg]:size-4"><Globe aria-hidden /></span>}
              name={`${LEVEL_LABEL[l.level]} · ${l.expires_at ? `until ${formatDate(l.expires_at)}` : "no expiry"}`}
              detail={[l.created_by ? `by ${l.created_by.name}` : null, l.last_used_at ? `opened ${formatRelative(l.last_used_at)}` : "not opened yet"].filter(Boolean).join(" · ")}
              trailing={
                <span className="flex items-center gap-2">
                  {!l.working ? (
                    <Badge tone="warning">
                      Paused
                    </Badge>
                  ) : null}
                  <Button
                    variant="danger-ghost"
                    size="sm"
                    loading={revoke.isPending && revoke.variables === l.id}
                    onClick={() =>
                      revoke.mutate(l.id, {
                        onSuccess: () => {
                          if (fresh?.id === l.id) setFresh(null);
                          toast.success("Link revoked");
                        },
                        onError: (e) => toast.error(errorMessage(e, "Couldn't revoke the link. Try again.")),
                      })
                    }
                  >
                    Revoke
                  </Button>
                </span>
              }
            />
          ))}
        </ul>
      ) : null}
      {data.links.some((l) => !l.working) ? (
        <p className="type-caption text-fg-muted">Paused links stopped working because whoever made them no longer has permission to share it publicly.</p>
      ) : null}
    </section>
  );
}
