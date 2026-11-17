"use client";

import {
  errorMessage,
  useAddForumModerator,
  useCreateForumCommunity,
  useForumAdminCommunities,
  useRemoveForumModerator,
  useUpdateForumCommunity,
  type ForumAdminCommunity,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, Dialog, EmptyState, ErrorState, Field, IconButton, Input, Switch, TextArea, toast } from "@scaffoldapp/ui-web";
import { Lock, MessagesSquare, Plus, X } from "lucide-react";
import { useState } from "react";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { PageHeader, Section } from "@/components/shell/PageHeader";

function Moderators({ c }: { c: ForumAdminCommunity }) {
  const add = useAddForumModerator();
  const remove = useRemoveForumModerator();
  const [email, setEmail] = useState("");
  const submit = async () => {
    if (!email.trim()) return;
    try {
      const person = await add.mutateAsync({ id: c.id, email: email.trim() });
      toast.success(`${person.display_name || "They"} can moderate ${c.title}`);
      setEmail("");
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't add them. Try again."));
    }
  };
  return (
    <Section title="Moderators" info="They pin, lock and remove posts and comments in this community. Every action goes to the audit log.">
      {c.moderators.length > 0 ? (
        <ul className="flex flex-col divide-y divide-border">
          {c.moderators.map((m) => (
            <li key={m.id} className="flex min-h-row items-center gap-3">
              <AppAvatar name={m.display_name || "?"} src={m.avatar_url} size="sm" />
              <span className="min-w-0 flex-1 truncate type-body text-fg">{m.display_name || "Unnamed"}</span>
              <IconButton
                label={`Remove ${m.display_name || "moderator"}`}
                icon={<X />}
                variant="ghost"
                size="sm"
                loading={remove.isPending && remove.variables?.userId === m.id}
                onClick={() => remove.mutate({ id: c.id, userId: m.id }, { onError: (e) => toast.error(errorMessage(e, "Couldn't remove them. Try again.")) })}
              />
            </li>
          ))}
        </ul>
      ) : (
        <p className="type-label text-fg-muted">No moderators yet. Staff moderate every community.</p>
      )}
      <form
        className="flex items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <Field label="Add by email" className="flex-1">
          <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="name@example.com" />
        </Field>
        <Button type="submit" variant="secondary" size="auto" loading={add.isPending} disabled={!email.trim()}>
          Add
        </Button>
      </form>
    </Section>
  );
}

function CommunityDialog({ value, onClose }: { value: ForumAdminCommunity | "new"; onClose: () => void }) {
  const isNew = value === "new";
  const c = isNew ? null : value;
  const create = useCreateForumCommunity();
  const update = useUpdateForumCommunity();
  const [slug, setSlug] = useState(c?.slug ?? "");
  const [title, setTitle] = useState(c?.title ?? "");
  const [description, setDescription] = useState(c?.description ?? "");
  const [rules, setRules] = useState((c?.rules ?? []).join("\n"));
  const [locked, setLocked] = useState(c?.locked ?? false);
  const busy = create.isPending || update.isPending;

  const save = async () => {
    const body = {
      slug: slug.trim().toLowerCase(),
      title: title.trim(),
      description: description.trim(),
      rules: rules
        .split("\n")
        .map((r) => r.trim())
        .filter(Boolean),
      locked,
    };
    try {
      if (c) await update.mutateAsync({ id: c.id, ...body });
      else await create.mutateAsync(body);
      toast.success(c ? "Community saved" : "Community created");
      onClose();
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't save the community. Try again."));
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && !busy && onClose()}
      title={c ? c.title : "New community"}
      size="lg"
      footer={
        <>
          <Button variant="ghost" size="auto" disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <Button size="auto" loading={busy} disabled={!slug.trim() || title.trim().length < 2} onClick={() => void save()}>
            {c ? "Save" : "Create"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-section">
        <div className="flex flex-col gap-4">
          <Field label="Name">
            <Input value={title} maxLength={100} onChange={(e) => setTitle(e.target.value)} />
          </Field>
          <Field label="Address" info="Lowercase letters, digits and hyphens: /forum/c/<address>.">
            <Input value={slug} maxLength={64} onChange={(e) => setSlug(e.target.value)} placeholder="bread-baking" />
          </Field>
          <Field label="Description" info="What it's about. It shows on the community and helps screening judge what's off-topic.">
            <TextArea value={description} maxLength={500} minRows={2} maxRows={6} autoGrow onChange={(e) => setDescription(e.target.value)} />
          </Field>
          <Field label="Rules" info="One per line, 10 at most. Empty uses the forum's default rules.">
            <TextArea value={rules} minRows={3} maxRows={10} autoGrow onChange={(e) => setRules(e.target.value)} />
          </Field>
          <Switch checked={locked} onCheckedChange={setLocked} label="Locked" info="Only moderators and staff can post or comment." />
        </div>
        {c ? <Moderators c={c} /> : null}
      </div>
    </Dialog>
  );
}

/** Admin → Forum: communities with their activity; create, edit, lock, and name community moderators. */
export function AdminForumScreen() {
  const list = useForumAdminCommunities();
  const [editing, setEditing] = useState<ForumAdminCommunity | "new" | null>(null);
  const rows = list.data?.items ?? [];
  const current = editing && editing !== "new" ? (rows.find((r) => r.id === editing.id) ?? editing) : editing;

  return (
    <div className="flex flex-col gap-section">
      <PageHeader
        title="Forum"
        info="Communities people post in. Reports and posts screening holds go to Moderation."
        action={
          <Button size="auto" leftIcon={<Plus />} onClick={() => setEditing("new")}>
            New community
          </Button>
        }
      />
      <Card padding="none">
        {list.isPending ? (
          <TableSkeleton rows={4} cols={4} />
        ) : list.isError ? (
          <ErrorState compact title="Couldn't load communities" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState
            compact
            icon={<MessagesSquare />}
            title="No communities yet"
            action={
              <Button variant="secondary" size="auto" onClick={() => setEditing("new")}>
                Create one
              </Button>
            }
          />
        ) : (
          <Table label="Communities">
            <THead>
              <TH>Community</TH>
              <TH className="hidden sm:table-cell">Moderators</TH>
              <TH align="right">This week</TH>
              <TH align="right" className="hidden sm:table-cell">
                Posts
              </TH>
            </THead>
            <tbody>
              {rows.map((c) => (
                <TR key={c.id} onActivate={() => setEditing(c)}>
                  <TD>
                    <span className="flex min-w-0 flex-col gap-1">
                      <span className="flex items-center gap-2">
                        <span className="truncate font-semibold text-fg">{c.title}</span>
                        {c.locked ? (
                          <Badge tone="neutral" size="sm">
                            <Lock className="size-3" aria-hidden />
                            Locked
                          </Badge>
                        ) : null}
                        {c.ref_kind ? (
                          <Badge tone="info" size="sm">
                            App
                          </Badge>
                        ) : null}
                      </span>
                      <span className="truncate type-caption text-fg-muted">{`/forum/c/${c.slug}`}</span>
                    </span>
                  </TD>
                  <TD className="hidden sm:table-cell">{c.moderators.length > 0 ? c.moderators.map((m) => m.display_name || "Unnamed").join(", ") : "Staff"}</TD>
                  <TD align="right">{c.posts_7d}</TD>
                  <TD align="right" className="hidden sm:table-cell">
                    {c.post_count}
                  </TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
      {current ? <CommunityDialog key={current === "new" ? "new" : current.id} value={current} onClose={() => setEditing(null)} /> : null}
    </div>
  );
}
