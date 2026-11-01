"use client";

import {
  errorMessage,
  reposRelativeTime,
  useCreateReposCredential,
  useCreateReposProtection,
  useDeleteReposProtection,
  useReposCredentials,
  useReposGitInfo,
  useReposProtections,
  useReposRefs,
  useRevokeReposCredential,
  useUpdateRepo,
  useUpdateReposProtection,
  type ReposCreatedCredential,
  type ReposProtection,
} from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, IconButton, Input, Select, SegmentedControl, Skeleton, SkeletonGroup, SkeletonRow, Stepper, Switch, TextArea, toast } from "@scaffoldapp/ui-web";
import { Copy, KeyRound, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen, Section } from "@/components/shell/PageHeader";
import { copyText, useRepoContext } from "./RepoLayout";

/** Repository settings (manage level): description, default branch, URL name, merging and branch protection. */
export function RepoSettingsScreen() {
  const repo = useRepoContext();
  const update = useUpdateRepo(repo.id);
  const refs = useReposRefs(repo.id);
  const [description, setDescription] = useState(repo.description);
  const [slug, setSlug] = useState(repo.slug);
  useEffect(() => {
    setDescription(repo.description);
    setSlug(repo.slug);
  }, [repo.description, repo.slug]);
  if (!repo.can_manage) {
    return (
      <Card>
        <ErrorState title="Only people who manage this repository change its settings" />
      </Card>
    );
  }
  const save = async (body: Parameters<typeof update.mutateAsync>[0], done = "Saved") => {
    try {
      await update.mutateAsync(body);
      toast.success(done);
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't save."));
    }
  };
  const branches = (refs.data?.items ?? []).filter((r) => r.kind === "branch");
  return (
    <div className="flex max-w-form flex-col gap-section">
      <Section title="General">
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            void save({ description, slug });
          }}
        >
          <Field label="Description">
            <TextArea value={description} onChange={(e) => setDescription(e.target.value)} maxLength={350} minRows={2} />
          </Field>
          <Field label="URL name" info="Part of the clone URL. Changing it breaks clones that use the old URL until people update their remote.">
            <Input value={slug} onChange={(e) => setSlug(e.target.value.toLowerCase())} className="font-mono" />
          </Field>
          <Button type="submit" className="self-start" loading={update.isPending} disabled={description === repo.description && slug === repo.slug}>
            Save
          </Button>
        </form>
        <Field label="Default branch" info="What people see first and where pull requests aim by default.">
          <Select
            value={repo.default_branch}
            onValueChange={(v) => void save({ default_branch: v }, `Default branch is now ${v}`)}
            options={branches.map((b) => ({ value: b.name, label: b.name }))}
            disabled={branches.length === 0}
          />
        </Field>
      </Section>
      <Section title="Merging">
        <div className="flex flex-col gap-3">
          <Switch checked={repo.allow_merge_commit} onCheckedChange={(v) => void save({ allow_merge_commit: v })} label="Allow merge commits" info="Keeps every commit of the branch, joined by a merge commit." />
          <Switch checked={repo.allow_squash} onCheckedChange={(v) => void save({ allow_squash: v })} label="Allow squash merging" info="Combines the branch's commits into one commit on the base." />
          <Switch checked={repo.delete_branch_on_merge} onCheckedChange={(v) => void save({ delete_branch_on_merge: v })} label="Delete branches after merging" info="Ticks “Delete branch” in the merge dialog by default." />
        </div>
      </Section>
      <ProtectionSection />
    </div>
  );
}

function ProtectionSection() {
  const repo = useRepoContext();
  const rules = useReposProtections(repo.id);
  const create = useCreateReposProtection(repo.id);
  const [pattern, setPattern] = useState("");
  return (
    <Section title="Branch protection" info="Rules apply to git pushes and to changes made in the app. Use * for a group of branches (release/*).">
      {rules.isPending ? (
        <SkeletonGroup label="Loading rules" className="flex flex-col gap-2">
          <Skeleton className="h-40 w-full" />
        </SkeletonGroup>
      ) : rules.isError ? (
        <Card>
          <ErrorState info={errorMessage(rules.error, "Try again.")} onRetry={() => void rules.refetch()} />
        </Card>
      ) : (
        <div className="flex flex-col gap-4">
          {rules.data.items.length === 0 ? <p className="type-label text-fg-muted">No branch is protected.</p> : null}
          {rules.data.items.map((r) => (
            <RuleCard key={r.id} rule={r} />
          ))}
          <form
            className="flex flex-wrap items-end gap-2"
            onSubmit={async (e) => {
              e.preventDefault();
              try {
                await create.mutateAsync({ pattern: pattern.trim() });
                setPattern("");
              } catch (err) {
                toast.error(errorMessage(err, "Couldn't add the rule."));
              }
            }}
          >
            <Field label="Protect a branch" className="min-w-0 flex-1">
              <Input value={pattern} onChange={(e) => setPattern(e.target.value)} placeholder="release/*" className="font-mono" />
            </Field>
            <Button type="submit" variant="outline" leftIcon={<Plus />} loading={create.isPending} disabled={!pattern.trim()}>
              Add rule
            </Button>
          </form>
        </div>
      )}
    </Section>
  );
}

function RuleCard({ rule: r }: { rule: ReposProtection }) {
  const repo = useRepoContext();
  const update = useUpdateReposProtection(repo.id);
  const del = useDeleteReposProtection(repo.id);
  const [removing, setRemoving] = useState(false);
  const set = (body: Partial<ReposProtection>) =>
    update.mutate({ protectionId: r.id, body }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save the rule.")) });
  return (
    <Card className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <ShieldCheck className="size-5 text-fg-muted" aria-hidden />
        <span className="min-w-0 flex-1 truncate font-mono type-body font-semibold">{r.pattern}</span>
        {r.pattern === repo.default_branch ? <Badge tone="primary">default</Badge> : null}
        <IconButton label={`Remove the rule for ${r.pattern}`} icon={<Trash2 />} variant="danger" size="sm" tooltip onClick={() => setRemoving(true)} />
      </div>
      <Switch checked={r.require_pull_request} onCheckedChange={(v) => set({ require_pull_request: v })} label="Changes go through pull requests" info="No direct pushes or commits: people open a pull request and merge it." />
      <Field label="Approvals to merge" info="From people who can edit the repository, not the author.">
        <Stepper value={r.required_approvals} min={0} max={10} onValueChange={(v: number) => set({ required_approvals: v })} />
      </Field>
      <Switch checked={r.dismiss_stale_approvals} onCheckedChange={(v) => set({ dismiss_stale_approvals: v })} label="New commits reset approvals" />
      <Switch checked={r.allow_force_push} onCheckedChange={(v) => set({ allow_force_push: v })} label="Allow force-pushes" info="Only matters when direct pushes are allowed." />
      <Switch checked={r.allow_deletion} onCheckedChange={(v) => set({ allow_deletion: v })} label="Allow deleting the branch" />
      <ConfirmDialog
        open={removing}
        onOpenChange={setRemoving}
        title={`Stop protecting ${r.pattern}?`}
        tone="danger"
        confirmLabel="Remove rule"
        onConfirm={() => del.mutateAsync(r.id)}
        errorText={(e) => errorMessage(e, "Couldn't remove the rule.")}
      >
        Anyone who can edit the repository could then push to it directly.
      </ConfirmDialog>
    </Card>
  );
}

// ---- git credentials -----------------------------------------------------------

/** Settings → Git credentials: passwords for git over HTTPS (read or read-write, optional expiry). */
export function GitCredentialsScreen() {
  const creds = useReposCredentials();
  const info = useReposGitInfo();
  const revoke = useRevokeReposCredential();
  const [creating, setCreating] = useState(false);
  const [revoking, setRevoking] = useState<string | null>(null);
  const rows = creds.data?.items ?? [];
  return (
    <Screen
      title="Git credentials"
      info="Use one as the password when git asks (any username). They work only for git clone, pull and push, never for signing in or for AI agents."
      action={
        <Button size="auto" leftIcon={<Plus />} onClick={() => setCreating(true)}>
          New credential
        </Button>
      }
    >
      <section className="flex flex-col gap-4">
        <Card padding="none" className="p-1">
          {creds.isPending ? (
            <SkeletonGroup label="Loading credentials" className="flex flex-col">
              <SkeletonRow />
              <SkeletonRow />
            </SkeletonGroup>
          ) : creds.isError ? (
            <ErrorState info={errorMessage(creds.error, "Try again.")} onRetry={() => void creds.refetch()} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={<KeyRound />} title="No git credentials" action={<Button variant="outline" onClick={() => setCreating(true)}>Make one</Button>} />
          ) : (
            <ul className="flex flex-col divide-y divide-border">
              {rows.map((c) => (
                <li key={c.id} className="flex min-h-tap-lg flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
                  <KeyRound className="size-5 shrink-0 text-fg-muted" aria-hidden />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate type-label font-semibold text-fg">{c.name}</span>
                    <span className="truncate type-caption text-fg-muted">
                      <span className="font-mono">{c.prefix}…</span> · {c.last_used_at ? `used ${reposRelativeTime(c.last_used_at)}` : "never used"} ·{" "}
                      {c.expires_at ? `expires ${new Date(c.expires_at).toLocaleDateString()}` : "no expiry"}
                    </span>
                  </span>
                  <Badge tone={c.access === "write" ? "warning" : "neutral"}>{c.access === "write" ? "Read and write" : "Read"}</Badge>
                  <IconButton label={`Revoke ${c.name}`} icon={<Trash2 />} variant="danger" size="sm" tooltip onClick={() => setRevoking(c.id)} />
                </li>
              ))}
            </ul>
          )}
        </Card>
        <LoadMore hasNextPage={!!creds.hasNextPage} isFetchingNextPage={creds.isFetchingNextPage} fetchNextPage={creds.fetchNextPage} />
        {info.data ? (
          <p className="type-caption text-fg-muted">
            Clone URLs start with <span className="font-mono">{info.data.base_url}/</span>. Server git {info.data.version}.
          </p>
        ) : null}
      </section>
      <NewCredentialDialog open={creating} onOpenChange={setCreating} />
      <ConfirmDialog
        open={!!revoking}
        onOpenChange={(o) => !o && setRevoking(null)}
        title="Revoke this credential?"
        tone="danger"
        confirmLabel="Revoke"
        onConfirm={() => (revoking ? revoke.mutateAsync(revoking) : undefined)}
        errorText={(e) => errorMessage(e, "Couldn't revoke it.")}
      >
        Git on any computer using it stops working at once.
      </ConfirmDialog>
    </Screen>
  );
}

const EXPIRY = [
  { value: "30", label: "30 days" },
  { value: "90", label: "90 days" },
  { value: "365", label: "1 year" },
  { value: "0", label: "Never" },
];

function NewCredentialDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const create = useCreateReposCredential();
  const info = useReposGitInfo();
  const [name, setName] = useState("");
  const [access, setAccess] = useState<"read" | "write">("write");
  const [expiry, setExpiry] = useState("90");
  const [made, setMade] = useState<ReposCreatedCredential | null>(null);
  const close = (o: boolean) => {
    onOpenChange(o);
    if (!o) {
      setMade(null);
      setName("");
    }
  };
  return (
    <Dialog
      open={open}
      onOpenChange={close}
      title={made ? "Copy your credential now" : "New git credential"}
      footer={
        made ? (
          <Button onClick={() => close(false)}>Done</Button>
        ) : (
          <>
            <Button variant="ghost" onClick={() => close(false)}>
              Cancel
            </Button>
            <Button
              loading={create.isPending}
              disabled={!name.trim()}
              onClick={async () => {
                try {
                  setMade(await create.mutateAsync({ name: name.trim(), access, expires_in_days: Number(expiry) || null }));
                } catch (e) {
                  toast.error(errorMessage(e, "Couldn't make the credential."));
                }
              }}
            >
              Make credential
            </Button>
          </>
        )
      }
    >
      {made ? (
        <div className="flex flex-col gap-4">
          <Banner tone="warning">It's shown only now. Store it in your password manager or git's credential helper.</Banner>
          <div className="flex items-center gap-2 rounded-md bg-sunken p-3">
            <code className="min-w-0 flex-1 break-all font-mono type-label text-fg">{made.secret}</code>
            <IconButton label="Copy credential" icon={<Copy />} variant="ghost" size="sm" onClick={() => void copyText(made.secret, "Credential copied")} />
          </div>
          {info.data ? (
            <p className="type-label text-fg-muted">
              Try it: <span className="font-mono text-fg">git clone {info.data.base_url}/&lt;workspace&gt;/&lt;repository&gt;.git</span>, then paste it as the password.
            </p>
          ) : null}
        </div>
      ) : (
        <form className="flex flex-col gap-4" onSubmit={(e) => e.preventDefault()}>
          <Field label="Name" info="Which computer or tool uses it, so you know what to revoke.">
            <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={60} autoFocus placeholder="Work laptop" />
          </Field>
          <Field label="Access">
            <SegmentedControl<"read" | "write">
              aria-label="Access"
              value={access}
              onValueChange={setAccess}
              options={[
                { value: "read", label: "Read (clone, pull)" },
                { value: "write", label: "Read and write (push)" },
              ]}
            />
          </Field>
          <Field label="Expires">
            <Select value={expiry} onValueChange={setExpiry} options={EXPIRY} />
          </Field>
        </form>
      )}
    </Dialog>
  );
}
