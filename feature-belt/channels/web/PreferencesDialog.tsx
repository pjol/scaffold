"use client";

import {
  errorMessage,
  useChannelsEmoji,
  useChannelsWorkspace,
  useCreateChannelsEmoji,
  useDeleteChannelsEmoji,
  useUpdateChannelsSettings,
  useUpdateChannelsUserPrefs,
  useUpload,
} from "@scaffoldapp/shared";
import { Button, Chip, Dialog, EmptyState, ErrorState, Field, IconButton, Input, Select, Skeleton, Switch, Tab, TabPanel, Tabs, TabsList, toast } from "@scaffoldapp/ui-web";
import { ImagePlus, Smile, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

const RETENTION_OPTIONS = [
  { value: "0", label: "Keep forever" },
  { value: "30", label: "30 days" },
  { value: "90", label: "90 days" },
  { value: "365", label: "1 year" },
  { value: "730", label: "2 years" },
];

function YouTab({ orgId }: { orgId: string }) {
  const ws = useChannelsWorkspace(orgId);
  const save = useUpdateChannelsUserPrefs(orgId);
  const [title, setTitle] = useState("");
  const [keyword, setKeyword] = useState("");
  const prefs = ws.data?.prefs;
  useEffect(() => {
    if (prefs) setTitle(prefs.title);
  }, [prefs?.title]);
  if (!prefs) return <Skeleton className="h-32 w-full" />;
  const addKeyword = () => {
    const k = keyword.trim().toLowerCase();
    if (!k || prefs.keywords.includes(k)) return;
    save.mutate({ keywords: [...prefs.keywords, k] }, { onSuccess: () => setKeyword(""), onError: (e) => toast.error(errorMessage(e)) });
  };
  return (
    <div className="flex flex-col gap-5">
      <Field label="Title" info="Shown on your profile card in this workspace.">
        <div className="flex gap-2">
          <Input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={80} placeholder="Product designer" wrapperClassName="flex-1" />
          <Button variant="secondary" disabled={title.trim() === prefs.title} loading={save.isPending} onClick={() => save.mutate({ title: title.trim() }, { onSuccess: () => toast.success("Title saved."), onError: (e) => toast.error(errorMessage(e)) })}>
            Save
          </Button>
        </div>
      </Field>
      <Field label="Notify me about these words" info="Messages with these words notify you like a mention (not in muted conversations).">
        <div className="flex flex-col gap-3">
          <div className="flex gap-2">
            <Input value={keyword} onChange={(e) => setKeyword(e.target.value)} onKeyDown={(e) => e.key === "Enter" && addKeyword()} maxLength={40} placeholder="release" wrapperClassName="flex-1" />
            <Button variant="secondary" disabled={keyword.trim().length < 2} onClick={addKeyword}>
              Add
            </Button>
          </div>
          {prefs.keywords.length > 0 && (
            <ul className="flex flex-wrap gap-2" aria-label="Keywords">
              {prefs.keywords.map((k) => (
                <li key={k}>
                  <Chip icon={<X aria-hidden />} aria-label={`Remove ${k}`} onClick={() => save.mutate({ keywords: prefs.keywords.filter((x) => x !== k) }, { onError: (e) => toast.error(errorMessage(e)) })}>
                    {k}
                  </Chip>
                </li>
              ))}
            </ul>
          )}
        </div>
      </Field>
    </div>
  );
}

function EmojiTab({ orgId }: { orgId: string }) {
  const emoji = useChannelsEmoji(orgId);
  const create = useCreateChannelsEmoji(orgId);
  const remove = useDeleteChannelsEmoji(orgId);
  const upload = useUpload();
  const fileRef = useRef<HTMLInputElement>(null);
  const [name, setName] = useState("");
  const [alias, setAlias] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const cleanName = (v: string) => v.toLowerCase().replace(/^:|:$/g, "").replace(/[^a-z0-9_+-]/g, "").slice(0, 40);
  const add = async () => {
    try {
      if (file) {
        const res = await upload.mutateAsync({ file, purpose: "channel_emoji", maxBytes: 256 * 1024 });
        await create.mutateAsync({ name, key: res.key });
      } else await create.mutateAsync({ name, alias_of: alias });
      toast.success(`:${name}: added.`);
      setName("");
      setAlias("");
      setFile(null);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  if (emoji.isPending) return <Skeleton className="h-32 w-full" />;
  if (emoji.isError) return <ErrorState compact title="Couldn't load emoji" info={emoji.error.message} onRetry={() => void emoji.refetch()} retrying={emoji.isFetching} />;
  return (
    <div className="flex flex-col gap-5">
      {emoji.data.can_add && (
        <div className="flex flex-col gap-3 rounded-md border border-border p-3">
          <Field label="Name">
            <Input value={name} onChange={(e) => setName(cleanName(e.target.value))} placeholder="party-parrot" leadingIcon={<span className="type-label text-fg-muted">:</span>} />
          </Field>
          <div className="flex flex-wrap items-center gap-2">
            <input ref={fileRef} type="file" accept="image/png,image/gif,image/jpeg,image/webp" className="hidden" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
            <Button variant="secondary" size="sm" leftIcon={<ImagePlus />} onClick={() => fileRef.current?.click()}>
              {file ? file.name : "Choose image"}
            </Button>
            <span className="type-caption text-fg-muted">or an alias of</span>
            <Input value={alias} onChange={(e) => setAlias(cleanName(e.target.value))} placeholder="existing-name" aria-label="Alias of" disabled={!!file} wrapperClassName="w-40" />
          </div>
          <Button className="w-fit" disabled={name.length < 2 || (!file && alias.length < 2)} loading={upload.isPending || create.isPending} onClick={() => void add()}>
            Add emoji
          </Button>
        </div>
      )}
      {emoji.data.items.length === 0 ? (
        <EmptyState compact icon={<Smile />} title="No custom emoji yet" />
      ) : (
        <ul className="flex max-h-72 flex-col gap-1 overflow-y-auto" aria-label="Custom emoji">
          {emoji.data.items.map((e) => (
            <li key={e.id} className="flex min-h-tap-sm items-center gap-3 rounded-md px-2">
              <img src={e.url} alt="" className="size-6 object-contain" />
              <span className="min-w-0 flex-1 truncate type-label text-fg">
                :{e.name}:{e.alias_of && <span className="text-fg-muted"> → :{e.alias_of}:</span>}
              </span>
              {e.created_by && <span className="hidden truncate type-caption text-fg-muted sm:inline">{e.created_by.display_name}</span>}
              {e.can_delete && <IconButton size="sm" variant="ghost" label={`Remove :${e.name}:`} icon={<Trash2 />} onClick={() => remove.mutate(e.id, { onError: (err) => toast.error(errorMessage(err)) })} />}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function WorkspaceTab({ orgId }: { orgId: string }) {
  const ws = useChannelsWorkspace(orgId);
  const save = useUpdateChannelsSettings(orgId);
  const settings = ws.data?.settings;
  if (!settings) return <Skeleton className="h-24 w-full" />;
  const retention = String(settings.retention_days ?? 0);
  return (
    <div className="flex flex-col gap-5">
      <Switch
        checked={settings.unfurl_links}
        onCheckedChange={(v) => save.mutate({ unfurl_links: v }, { onError: (e) => toast.error(errorMessage(e)) })}
        label="Show previews of links to other sites"
        info="Previews are fetched by the server and stored, so readers never contact the linked site."
      />
      <Field label="Keep messages" info="Older messages and their files are deleted for good. Channels can override this. Changes are recorded in the audit log.">
        <Select
          value={RETENTION_OPTIONS.some((o) => o.value === retention) ? retention : "custom"}
          onValueChange={(v) => v !== "custom" && save.mutate({ retention_days: Number(v) }, { onSuccess: () => toast.success("Retention saved."), onError: (e) => toast.error(errorMessage(e)) })}
          options={RETENTION_OPTIONS.some((o) => o.value === retention) ? RETENTION_OPTIONS : [...RETENTION_OPTIONS, { value: "custom", label: `${retention} days` }]}
        />
      </Field>
    </div>
  );
}

/** Channels preferences: yours (title, keywords), the workspace's emoji, and admin settings. */
export function PreferencesDialog({ orgId, open, onOpenChange }: { orgId: string; open: boolean; onOpenChange: (o: boolean) => void }) {
  const ws = useChannelsWorkspace(orgId);
  const [tab, setTab] = useState("you");
  const admin = ws.data?.permissions.manage_workspace ?? false;
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Channel preferences" size="lg">
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList aria-label="Preferences">
          <Tab value="you">You</Tab>
          <Tab value="emoji">Emoji</Tab>
          {admin && <Tab value="workspace">Workspace</Tab>}
        </TabsList>
        <TabPanel value="you" className="pt-4">
          <YouTab orgId={orgId} />
        </TabPanel>
        <TabPanel value="emoji" className="pt-4">
          <EmojiTab orgId={orgId} />
        </TabPanel>
        {admin && (
          <TabPanel value="workspace" className="pt-4">
            <WorkspaceTab orgId={orgId} />
          </TabPanel>
        )}
      </Tabs>
    </Dialog>
  );
}
