"use client";

import { channelsNameFrom, errorMessage, hasErrorCode, orgsNoun, presenceLabel, useChannelsPeople, useChannelsWorkspace, useCreateChannel, useDebouncedValue, useOpenChannelsDM, useUserPresence, type ChannelsPerson } from "@scaffoldapp/shared";
import { Button, Chip, Dialog, EmptyState, ErrorState, Field, Input, SegmentedControl, SkeletonRow, Switch, cn, toast } from "@scaffoldapp/ui-web";
import { Check, Search, Users, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { PresenceAvatar } from "@/features/presence";

function PersonRow({ person, selected, disabled, onToggle }: { person: ChannelsPerson; selected: boolean; disabled: boolean; onToggle: () => void }) {
  const presence = useUserPresence(person.id);
  return (
    <button
      type="button"
      role="option"
      aria-selected={selected}
      disabled={disabled && !selected}
      onClick={onToggle}
      className={cn("flex min-h-tap-xl w-full items-center gap-3 rounded-md px-3 py-2 text-left transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-50", selected && "bg-primary-soft hover:bg-primary-soft")}
    >
      <PresenceAvatar userId={person.id} name={person.display_name} src={person.avatar_url} size="sm" />
      <span className="flex min-w-0 flex-1 items-baseline gap-2">
        <span className="truncate type-body text-fg">{person.display_name}</span>
        <span className="sr-only">{presenceLabel(presence)}</span>
      </span>
      {selected && <Check aria-hidden className="size-5 text-primary-strong" />}
    </button>
  );
}

/** Search org members and pick some (chips above the list). */
export function PeoplePicker({ orgId, selected, onChange, max, exclude = [], channelId }: { orgId: string; selected: ChannelsPerson[]; onChange: (p: ChannelsPerson[]) => void; max: number; exclude?: string[]; channelId?: string }) {
  const [q, setQ] = useState("");
  const query = useDebouncedValue(q, 200);
  const people = useChannelsPeople(orgId, query, channelId);
  const list = (people.data?.items ?? []).filter((p) => !exclude.includes(p.id) && !(channelId && p.in_channel));
  const ids = new Set(selected.map((p) => p.id));
  const toggle = (p: ChannelsPerson) => onChange(ids.has(p.id) ? selected.filter((x) => x.id !== p.id) : [...selected, p]);
  return (
    <div className="flex flex-col gap-3">
      {selected.length > 0 && (
        <ul className="flex flex-wrap gap-2" aria-label="Chosen">
          {selected.map((p) => (
            <li key={p.id}>
              <Chip selected icon={<X aria-hidden />} onClick={() => toggle(p)} aria-label={`Remove ${p.display_name}`}>
                {p.display_name}
              </Chip>
            </li>
          ))}
        </ul>
      )}
      <Input autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search by name or email" aria-label="Search people" leadingIcon={<Search />} />
      <div className="max-h-80 overflow-y-auto" role="listbox" aria-multiselectable aria-label="People">
        {people.isPending ? (
          <div className="flex flex-col gap-1" aria-hidden>
            {[0, 1, 2].map((i) => (
              <SkeletonRow key={i} />
            ))}
          </div>
        ) : people.isError ? (
          <ErrorState compact title="Couldn't load people" info={people.error.message} onRetry={() => void people.refetch()} retrying={people.isFetching} />
        ) : list.length === 0 ? (
          <EmptyState compact icon={<Users />} title={q ? "Nobody matches" : "No one else here yet"} />
        ) : (
          list.map((p) => <PersonRow key={p.id} person={p} selected={ids.has(p.id)} disabled={selected.length >= max} onToggle={() => toggle(p)} />)
        )}
      </div>
    </div>
  );
}

/** New message: one person (a DM), several (a group DM), or nobody (a note to yourself). */
export function NewMessageDialog({ orgId, open, onOpenChange }: { orgId: string; open: boolean; onOpenChange: (o: boolean) => void }) {
  const router = useRouter();
  const ws = useChannelsWorkspace(orgId);
  const openDM = useOpenChannelsDM(orgId);
  const [selected, setSelected] = useState<ChannelsPerson[]>([]);
  useEffect(() => {
    if (open) setSelected([]);
  }, [open]);
  const max = (ws.data?.config.max_group_dm ?? 9) - 1;
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="New message"
      info={`Message one person, or up to ${max} people at once. For more, create a private channel.`}
      footer={
        <Button
          disabled={selected.length === 0}
          loading={openDM.isPending}
          onClick={() =>
            openDM.mutate(
              selected.map((p) => p.id),
              {
                onSuccess: (d) => {
                  onOpenChange(false);
                  router.push(`/channels/${d.id}`);
                },
                onError: (e) => toast.error(errorMessage(e, "Couldn't open the conversation.")),
              },
            )
          }
        >
          {selected.length > 1 ? "Start group message" : "Start conversation"}
        </Button>
      }
    >
      <PeoplePicker orgId={orgId} selected={selected} onChange={setSelected} max={max} />
    </Dialog>
  );
}

/** Create a channel: name, public or private, chat or forum, topic, and default for admins. */
export function NewChannelDialog({ orgId, open, onOpenChange }: { orgId: string; open: boolean; onOpenChange: (o: boolean) => void }) {
  const router = useRouter();
  const ws = useChannelsWorkspace(orgId);
  const create = useCreateChannel(orgId);
  const perms = ws.data?.permissions;
  const [name, setName] = useState("");
  const [kind, setKind] = useState<"public" | "private">("public");
  const [layout, setLayout] = useState<"chat" | "forum">("chat");
  const [topic, setTopic] = useState("");
  const [tags, setTags] = useState("");
  const [isDefault, setIsDefault] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!open) return;
    setName("");
    setTopic("");
    setTags("");
    setError(null);
    setIsDefault(false);
    setLayout("chat");
    setKind(perms?.create_public === false ? "private" : "public");
  }, [open, perms?.create_public]);
  const submit = () => {
    const n = channelsNameFrom(name);
    if (!n) return setError("Enter a name.");
    create.mutate(
      {
        name: n,
        kind,
        layout,
        topic,
        ...(layout === "forum" ? { forum_tags: tags.split(",").map((t) => t.trim()).filter(Boolean) } : {}),
        ...(isDefault && kind === "public" ? { is_default: true } : {}),
      },
      {
        onSuccess: (d) => {
          onOpenChange(false);
          toast.success(`#${d.name} created.`);
          router.push(`/channels/${d.id}`);
        },
        onError: (e) => (hasErrorCode(e, "conflict") || hasErrorCode(e, "invalid_request") ? setError(errorMessage(e)) : toast.error(errorMessage(e, "Couldn't create the channel."))),
      },
    );
  };
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="New channel"
      size="lg"
      footer={
        <Button loading={create.isPending} disabled={!channelsNameFrom(name)} onClick={submit}>
          Create
        </Button>
      }
    >
      <form
        className="flex flex-col gap-5"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Field label="Name" error={error ?? undefined} info="Lowercase letters, numbers, dashes and underscores. Spaces become dashes.">
          <Input
            autoFocus
            value={name}
            maxLength={80}
            onChange={(e) => {
              setName(channelsNameFrom(e.target.value));
              setError(null);
            }}
            placeholder="project-updates"
            leadingIcon={<span aria-hidden className="type-body text-fg-muted">#</span>}
          />
        </Field>
        <Field label="Who can find it" info={`Anyone in the ${orgsNoun()} can browse and join a public channel. A private one is by invitation and stays hidden from everyone else.`}>
          <SegmentedControl<"public" | "private">
            fullWidth
            aria-label="Visibility"
            value={kind}
            onValueChange={setKind}
            options={[
              { value: "public", label: "Public", disabled: perms?.create_public === false },
              { value: "private", label: "Private", disabled: perms?.create_private === false },
            ]}
          />
        </Field>
        <Field label="Layout" info="Chat is a running conversation. A forum is a list of titled posts, each with its own thread.">
          <SegmentedControl<"chat" | "forum">
            fullWidth
            aria-label="Layout"
            value={layout}
            onValueChange={setLayout}
            options={[
              { value: "chat", label: "Chat" },
              { value: "forum", label: "Forum" },
            ]}
          />
        </Field>
        {layout === "forum" && (
          <Field label="Tags" info="Comma-separated, up to 20. Posts can carry them and the list filters by them.">
            <Input value={tags} onChange={(e) => setTags(e.target.value)} placeholder="Question, Idea, Announcement" />
          </Field>
        )}
        <Field label="Topic">
          <Input value={topic} maxLength={250} onChange={(e) => setTopic(e.target.value)} />
        </Field>
        {perms?.set_default && kind === "public" && (
          <Switch checked={isDefault} onCheckedChange={setIsDefault} label="Everyone joins it" info={`New and current members of the ${orgsNoun()} are added once. They can leave.`} />
        )}
      </form>
    </Dialog>
  );
}

/** Add org members to a channel. */
export function AddPeopleDialog({ orgId, channelId, channelName, open, onOpenChange, onAdd, busy }: { orgId: string; channelId: string; channelName: string; open: boolean; onOpenChange: (o: boolean) => void; onAdd: (ids: string[]) => Promise<unknown>; busy: boolean }) {
  const [selected, setSelected] = useState<ChannelsPerson[]>([]);
  useEffect(() => {
    if (open) setSelected([]);
  }, [open]);
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Add people to ${channelName}`}
      footer={
        <Button
          disabled={selected.length === 0}
          loading={busy}
          onClick={() =>
            onAdd(selected.map((p) => p.id)).then(
              () => onOpenChange(false),
              (e: unknown) => toast.error(errorMessage(e, "Couldn't add them.")),
            )
          }
        >
          Add
        </Button>
      }
    >
      <PeoplePicker orgId={orgId} channelId={channelId} selected={selected} onChange={setSelected} max={500} />
    </Dialog>
  );
}
