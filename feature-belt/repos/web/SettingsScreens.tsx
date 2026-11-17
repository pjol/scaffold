"use client";

import {
  errorMessage,
  reposRelativeTime,
  useCreateReposCIToken,
  useCreateReposCredential,
  useCreateReposProtection,
  useDeleteReposProtection,
  useDeleteReposWebhook,
  usePingReposWebhook,
  useRedeliverReposWebhook,
  useReposCITokens,
  useReposCredentials,
  useReposDeliveries,
  useReposDelivery,
  useReposGitInfo,
  useReposProtections,
  useReposRefs,
  useReposWebhooks,
  useRevokeReposCIToken,
  useRevokeReposCredential,
  useSaveReposWebhook,
  useUpdateRepo,
  useUpdateReposProtection,
  type ReposCreatedCIToken,
  type ReposCreatedCredential,
  type ReposProtection,
  type ReposWebhook,
  type ReposWebhookDelivery,
  type ReposWebhookEvent,
} from "@scaffoldapp/shared";
import {
  Badge,
  Banner,
  Button,
  Card,
  Checkbox,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  Menu,
  MenuItem,
  MenuSeparator,
  Select,
  SegmentedControl,
  Sheet,
  Skeleton,
  SkeletonGroup,
  SkeletonRow,
  Stepper,
  Switch,
  TextArea,
  toast,
} from "@scaffoldapp/ui-web";
import { ArrowLeft, Bot, Copy, KeyRound, ListChecks, MoreHorizontal, Pencil, Plus, Radio, RotateCcw, ShieldCheck, Trash2, Webhook as WebhookIcon, X } from "lucide-react";
import { useEffect, useState, type KeyboardEvent } from "react";
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
          <Switch
            checked={!!repo.allow_rebase}
            onCheckedChange={(v) => void save({ allow_rebase: v })}
            label="Allow rebase merging"
            info="Replays the branch's commits on top of the base, one by one, without a merge commit. Branches with merge commits or conflicting steps can't rebase."
          />
          <Switch
            checked={!!repo.allow_auto_merge}
            onCheckedChange={(v) => void save({ allow_auto_merge: v })}
            label="Allow auto-merge"
            info="People who can edit turn it on for a pull request; it merges by itself, as them, once approvals and required checks pass."
          />
          <Switch checked={repo.delete_branch_on_merge} onCheckedChange={(v) => void save({ delete_branch_on_merge: v })} label="Delete branches after merging" info="Ticks “Delete branch” in the merge dialog by default." />
        </div>
      </Section>
      <ProtectionSection />
      <CITokensSection />
      <WebhooksSection />
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
      <Switch
        checked={r.require_code_owner_review}
        onCheckedChange={(v) => set({ require_code_owner_review: v })}
        label="Require review from code owners"
        info="Each changed file needs an approval from one of its owners in CODEOWNERS (.github/CODEOWNERS, CODEOWNERS or docs/CODEOWNERS on the base branch). Owners are workspace emails or @team handles."
      />
      <Field label="Required checks" info="Status contexts CI posts (build, test). Each must succeed on the latest commit before merging.">
        <ChecksInput value={r.required_checks ?? []} onChange={(v) => set({ required_checks: v })} label={`Required checks for ${r.pattern}`} />
      </Field>
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

/** Status context names as removable chips; Enter or comma adds one. */
function ChecksInput({ value, onChange, label }: { value: string[]; onChange: (v: string[]) => void; label: string }) {
  const [draft, setDraft] = useState("");
  const add = () => {
    const parts = draft
      .split(",")
      .map((p) => p.trim())
      .filter(Boolean);
    if (parts.length === 0) return;
    const next = [...value];
    for (const p of parts) if (!next.includes(p) && next.length < 20) next.push(p);
    onChange(next);
    setDraft("");
  };
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      add();
    } else if (e.key === "Backspace" && !draft && value.length > 0) {
      onChange(value.slice(0, -1));
    }
  };
  return (
    <div className="flex flex-col gap-2">
      {value.length > 0 ? (
        <ul className="flex flex-wrap gap-2" aria-label={label}>
          {value.map((c) => (
            <li key={c} className="flex items-center gap-1 rounded-pill bg-sunken py-0.5 pl-3 pr-1 font-mono type-label text-fg">
              {c}
              <button
                type="button"
                aria-label={`Stop requiring ${c}`}
                className="flex size-6 items-center justify-center rounded-pill text-fg-muted hover:bg-surface hover:text-fg focus-visible:outline-2 focus-visible:outline-ring [&_svg]:size-3.5"
                onClick={() => onChange(value.filter((x) => x !== c))}
              >
                <X aria-hidden />
              </button>
            </li>
          ))}
        </ul>
      ) : null}
      <Input
        aria-label={`Add a required check for ${label.replace(/^Required checks for /, "")}`}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={onKey}
        onBlur={add}
        placeholder={value.length ? "Add another context" : "build"}
        className="font-mono"
      />
    </div>
  );
}

// ---- CI tokens -----------------------------------------------------------------

function CITokensSection() {
  const repo = useRepoContext();
  const tokens = useReposCITokens(repo.id);
  const revoke = useRevokeReposCIToken(repo.id);
  const [creating, setCreating] = useState(false);
  const [revoking, setRevoking] = useState<string | null>(null);
  return (
    <Section
      title="CI tokens"
      info="CI posts commit statuses (passing, failing, running) with one of these. A token works only for this repository's status route: never for git, signing in or AI agents."
      action={
        <Button size="sm" variant="outline" leftIcon={<Plus />} onClick={() => setCreating(true)}>
          New token
        </Button>
      }
    >
      <Card padding="none" className="p-1">
        {tokens.isPending ? (
          <SkeletonGroup label="Loading CI tokens" className="flex flex-col">
            <SkeletonRow />
          </SkeletonGroup>
        ) : tokens.isError ? (
          <ErrorState info={errorMessage(tokens.error, "Try again.")} onRetry={() => void tokens.refetch()} />
        ) : tokens.data.items.length === 0 ? (
          <EmptyState compact icon={<Bot />} title="No CI tokens" />
        ) : (
          <ul className="flex flex-col divide-y divide-border">
            {tokens.data.items.map((t) => (
              <li key={t.id} className="flex min-h-tap-lg items-center gap-3 px-3 py-2">
                <Bot className="size-5 shrink-0 text-fg-muted" aria-hidden />
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate type-label font-semibold text-fg">{t.name}</span>
                  <span className="truncate type-caption text-fg-muted">
                    <span className="font-mono">{t.prefix}…</span> · {t.last_used_at ? `used ${reposRelativeTime(t.last_used_at)}` : "never used"}
                  </span>
                </span>
                <IconButton label={`Revoke ${t.name}`} icon={<Trash2 />} variant="danger" size="sm" tooltip onClick={() => setRevoking(t.id)} />
              </li>
            ))}
          </ul>
        )}
      </Card>
      {creating ? <NewCITokenDialog statusUrl={tokens.data?.status_url ?? ""} onClose={() => setCreating(false)} /> : null}
      <ConfirmDialog
        open={!!revoking}
        onOpenChange={(o) => !o && setRevoking(null)}
        title="Revoke this CI token?"
        tone="danger"
        confirmLabel="Revoke"
        onConfirm={() => (revoking ? revoke.mutateAsync(revoking) : undefined)}
        errorText={(e) => errorMessage(e, "Couldn't revoke it.")}
      >
        CI using it can't post statuses until you give it a new one. Statuses it posted stay.
      </ConfirmDialog>
    </Section>
  );
}

function NewCITokenDialog({ statusUrl, onClose }: { statusUrl: string; onClose: () => void }) {
  const repo = useRepoContext();
  const create = useCreateReposCIToken(repo.id);
  const [name, setName] = useState("");
  const [made, setMade] = useState<ReposCreatedCIToken | null>(null);
  const curl = made
    ? `curl -X POST ${statusUrl.replace("{sha}", "$COMMIT_SHA")} \\\n  -H "Authorization: Bearer ${made.secret}" \\\n  -H "Content-Type: application/json" \\\n  -d '{"state":"success","context":"build","description":"Built","target_url":"https://ci.example.com/runs/1"}'`
    : "";
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={made ? "Copy your CI token now" : "New CI token"}
      size={made ? "lg" : "md"}
      footer={
        made ? (
          <Button onClick={onClose}>Done</Button>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button
              loading={create.isPending}
              disabled={!name.trim()}
              onClick={async () => {
                try {
                  setMade(await create.mutateAsync(name.trim()));
                } catch (e) {
                  toast.error(errorMessage(e, "Couldn't make the token."));
                }
              }}
            >
              Make token
            </Button>
          </>
        )
      }
    >
      {made ? (
        <div className="flex flex-col gap-4">
          <Banner tone="warning">It's shown only now. Store it as a secret in your CI.</Banner>
          <div className="flex items-center gap-2 rounded-md bg-sunken p-3">
            <code className="min-w-0 flex-1 break-all font-mono type-label text-fg">{made.secret}</code>
            <IconButton label="Copy token" icon={<Copy />} variant="ghost" size="sm" onClick={() => void copyText(made.secret, "Token copied")} />
          </div>
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              <span className="flex-1 type-label font-semibold text-fg">Post a status from CI</span>
              <IconButton label="Copy the example" icon={<Copy />} variant="ghost" size="sm" onClick={() => void copyText(curl, "Example copied")} />
            </div>
            <pre className="overflow-x-auto rounded-md bg-sunken p-3 font-mono type-caption text-fg">{curl}</pre>
            <p className="type-caption text-fg-muted">state is pending, success, failure or error; context names the job (one status per context and commit).</p>
          </div>
        </div>
      ) : (
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
          }}
        >
          <Field label="Name" info="The CI that uses it, so you know what to revoke.">
            <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={60} autoFocus placeholder="Build server" />
          </Field>
        </form>
      )}
    </Dialog>
  );
}

// ---- webhooks ------------------------------------------------------------------

const EVENT_LABELS: Record<ReposWebhookEvent, string> = {
  push: "Pushes",
  pull_request: "Pull requests",
  pull_request_review: "Reviews",
  issues: "Issues",
  issue_comment: "Issue comments",
  release: "Releases",
  status: "Commit statuses",
};

function statusTone(code: number | null | undefined): "success" | "danger" | "neutral" {
  if (!code) return "neutral";
  return code >= 200 && code < 300 ? "success" : "danger";
}

function WebhooksSection() {
  const repo = useRepoContext();
  const hooks = useReposWebhooks(repo.id);
  const del = useDeleteReposWebhook(repo.id);
  const ping = usePingReposWebhook(repo.id);
  const save = useSaveReposWebhook(repo.id);
  const [editing, setEditing] = useState<ReposWebhook | "new" | null>(null);
  const [deleting, setDeleting] = useState<ReposWebhook | null>(null);
  const [log, setLog] = useState<ReposWebhook | null>(null);
  return (
    <Section
      title="Webhooks"
      info="Each event is POSTed as JSON to the URL, signed with the hook's secret: X-Webhook-Signature-256 (and X-Hub-Signature-256) is sha256= the HMAC-SHA256 of the body."
      action={
        <Button size="sm" variant="outline" leftIcon={<Plus />} onClick={() => setEditing("new")}>
          Add webhook
        </Button>
      }
    >
      <Card padding="none" className="p-1">
        {hooks.isPending ? (
          <SkeletonGroup label="Loading webhooks" className="flex flex-col">
            <SkeletonRow />
          </SkeletonGroup>
        ) : hooks.isError ? (
          <ErrorState info={errorMessage(hooks.error, "Try again.")} onRetry={() => void hooks.refetch()} />
        ) : hooks.data.items.length === 0 ? (
          <EmptyState compact icon={<WebhookIcon />} title="No webhooks" />
        ) : (
          <ul className="flex flex-col divide-y divide-border">
            {hooks.data.items.map((h) => (
              <li key={h.id} className="flex min-h-tap-lg flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
                <WebhookIcon className="size-5 shrink-0 text-fg-muted" aria-hidden />
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate font-mono type-label font-semibold text-fg">{h.url}</span>
                  <span className="truncate type-caption text-fg-muted">
                    {h.events.map((e) => EVENT_LABELS[e] ?? e).join(", ")}
                    {h.last_delivery_at ? ` · last delivery ${reposRelativeTime(h.last_delivery_at)}` : " · no deliveries yet"}
                  </span>
                </span>
                {!h.active ? <Badge tone="neutral">Paused</Badge> : null}
                {h.last_status !== null ? <Badge tone={statusTone(h.last_status)}>{h.last_status === 0 ? "Failed" : h.last_status}</Badge> : null}
                <Button size="sm" variant="ghost" leftIcon={<ListChecks />} onClick={() => setLog(h)}>
                  Deliveries
                </Button>
                <Menu trigger={<IconButton label={`More for ${h.url}`} icon={<MoreHorizontal />} variant="ghost" size="sm" />}>
                  <MenuItem icon={<Pencil />} onSelect={() => setEditing(h)}>
                    Edit
                  </MenuItem>
                  <MenuItem
                    icon={<Radio />}
                    onSelect={() =>
                      ping.mutate(h.id, {
                        onSuccess: () => toast.success("Ping sent. Its delivery shows in the log."),
                        onError: (e) => toast.error(errorMessage(e, "Couldn't send the ping.")),
                      })
                    }
                  >
                    Send a ping
                  </MenuItem>
                  <MenuItem
                    icon={<RotateCcw />}
                    onSelect={() =>
                      save.mutate({ hookId: h.id, body: { active: !h.active } }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save the webhook.")) })
                    }
                  >
                    {h.active ? "Pause deliveries" : "Resume deliveries"}
                  </MenuItem>
                  <MenuSeparator />
                  <MenuItem icon={<Trash2 />} destructive onSelect={() => setDeleting(h)}>
                    Delete
                  </MenuItem>
                </Menu>
              </li>
            ))}
          </ul>
        )}
      </Card>
      {editing ? <WebhookDialog hook={editing === "new" ? null : editing} events={hooks.data?.events ?? (Object.keys(EVENT_LABELS) as ReposWebhookEvent[])} onClose={() => setEditing(null)} /> : null}
      {log ? <DeliveriesSheet hook={log} onClose={() => setLog(null)} /> : null}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title="Delete this webhook?"
        tone="danger"
        confirmLabel="Delete webhook"
        onConfirm={() => (deleting ? del.mutateAsync(deleting.id) : undefined)}
        errorText={(e) => errorMessage(e, "Couldn't delete it.")}
      >
        Deliveries to {deleting?.url} stop, and its delivery log goes.
      </ConfirmDialog>
    </Section>
  );
}

function WebhookDialog({ hook, events, onClose }: { hook: ReposWebhook | null; events: ReposWebhookEvent[]; onClose: () => void }) {
  const repo = useRepoContext();
  const save = useSaveReposWebhook(repo.id);
  const [url, setUrl] = useState(hook?.url ?? "");
  const [chosen, setChosen] = useState<ReposWebhookEvent[]>(hook?.events ?? ["push"]);
  const [active, setActive] = useState(hook?.active ?? true);
  const [made, setMade] = useState<ReposWebhook | null>(null);
  const toggle = (e: ReposWebhookEvent, on: boolean) => setChosen((c) => (on ? [...c.filter((x) => x !== e), e] : c.filter((x) => x !== e)));
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={made ? "Copy the webhook secret now" : hook ? "Edit webhook" : "Add a webhook"}
      footer={
        made ? (
          <Button onClick={onClose}>Done</Button>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button
              loading={save.isPending}
              disabled={!url.trim() || chosen.length === 0}
              onClick={async () => {
                try {
                  const res = await save.mutateAsync({ ...(hook ? { hookId: hook.id } : {}), body: { url: url.trim(), events: chosen, active } });
                  if (hook) {
                    toast.success("Webhook saved");
                    onClose();
                  } else {
                    setMade(res);
                  }
                } catch (e) {
                  toast.error(errorMessage(e, "Couldn't save the webhook."));
                }
              }}
            >
              {hook ? "Save" : "Add webhook"}
            </Button>
          </>
        )
      }
    >
      {made ? (
        <div className="flex flex-col gap-4">
          <Banner tone="warning">It's shown only now. The receiver checks each delivery's signature with it.</Banner>
          <div className="flex items-center gap-2 rounded-md bg-sunken p-3">
            <code className="min-w-0 flex-1 break-all font-mono type-label text-fg">{made.secret}</code>
            <IconButton label="Copy secret" icon={<Copy />} variant="ghost" size="sm" onClick={() => void copyText(made.secret ?? "", "Secret copied")} />
          </div>
          <p className="type-label text-fg-muted">A ping is on its way to check the URL. Its delivery shows in the log.</p>
        </div>
      ) : (
        <form className="flex flex-col gap-4" onSubmit={(e) => e.preventDefault()}>
          <Field label="Payload URL" info="A public https address. Deliveries time out after 10 seconds and don't follow redirects.">
            <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/hooks/repos" inputMode="url" autoFocus className="font-mono" />
          </Field>
          <fieldset className="flex flex-col gap-2">
            <legend className="mb-2 type-label font-semibold text-fg">Events</legend>
            {events.map((e) => (
              <Checkbox key={e} checked={chosen.includes(e)} onCheckedChange={(on) => toggle(e, on)} label={EVENT_LABELS[e] ?? e} />
            ))}
          </fieldset>
          <Switch checked={active} onCheckedChange={setActive} label="Deliver events" info="Off pauses deliveries without deleting the webhook." />
        </form>
      )}
    </Dialog>
  );
}

function DeliveriesSheet({ hook, onClose }: { hook: ReposWebhook; onClose: () => void }) {
  const repo = useRepoContext();
  const deliveries = useReposDeliveries(repo.id, hook.id);
  const [open, setOpen] = useState<string | null>(null);
  const rows = deliveries.data?.items ?? [];
  return (
    <Sheet open onOpenChange={(o) => !o && onClose()} title={open ? "Delivery" : "Deliveries"} info={hook.url} size="lg">
      {open ? (
        <DeliveryDetail hookId={hook.id} deliveryId={open} onBack={() => setOpen(null)} />
      ) : deliveries.isPending ? (
        <SkeletonGroup label="Loading deliveries" className="flex flex-col">
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </SkeletonGroup>
      ) : deliveries.isError ? (
        <ErrorState info={errorMessage(deliveries.error, "Try again.")} onRetry={() => void deliveries.refetch()} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={<ListChecks />} title="No deliveries yet" />
      ) : (
        <div className="flex flex-col gap-3">
          <ul className="flex flex-col divide-y divide-border">
            {rows.map((d) => (
              <li key={d.id}>
                <button
                  type="button"
                  onClick={() => setOpen(d.id)}
                  className="flex min-h-tap-lg w-full items-center gap-3 rounded-sm px-2 py-2 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                >
                  <DeliveryBadge d={d} />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate type-label font-semibold text-fg">
                      {d.event}
                      {d.action ? ` · ${d.action}` : ""}
                      {d.redelivery_of ? <span className="font-normal text-fg-muted"> · redelivery</span> : null}
                    </span>
                    <span className="truncate type-caption text-fg-muted">
                      {reposRelativeTime(d.created_at)} · {d.duration_ms} ms{d.error ? ` · ${d.error}` : ""}
                    </span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
          <LoadMore hasNextPage={!!deliveries.hasNextPage} isFetchingNextPage={deliveries.isFetchingNextPage} fetchNextPage={deliveries.fetchNextPage} />
        </div>
      )}
    </Sheet>
  );
}

function DeliveryBadge({ d }: { d: Pick<ReposWebhookDelivery, "status_code" | "error"> }) {
  if (d.status_code === null && !d.error) return <Badge tone="neutral">Sending</Badge>;
  return <Badge tone={statusTone(d.status_code)}>{d.status_code ?? "Failed"}</Badge>;
}

function prettyJSON(s: string | undefined): string {
  if (!s) return "";
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

function DeliveryDetail({ hookId, deliveryId, onBack }: { hookId: string; deliveryId: string; onBack: () => void }) {
  const repo = useRepoContext();
  const d = useReposDelivery(repo.id, hookId, deliveryId);
  const redeliver = useRedeliverReposWebhook(repo.id, hookId);
  return (
    <div className="flex flex-col gap-4">
      <Button variant="ghost" size="sm" leftIcon={<ArrowLeft />} className="self-start" onClick={onBack}>
        All deliveries
      </Button>
      {d.isPending ? (
        <Skeleton className="h-64 w-full" />
      ) : d.isError ? (
        <ErrorState info={errorMessage(d.error, "Try again.")} onRetry={() => void d.refetch()} />
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-3">
            <DeliveryBadge d={d.data} />
            <span className="min-w-0 flex-1 type-label text-fg">
              {d.data.event}
              {d.data.action ? ` · ${d.data.action}` : ""} · {new Date(d.data.created_at).toLocaleString()} · {d.data.duration_ms} ms
            </span>
            <Button
              size="sm"
              variant="outline"
              leftIcon={<RotateCcw />}
              loading={redeliver.isPending}
              onClick={() =>
                redeliver.mutate(deliveryId, {
                  onSuccess: () => {
                    toast.success("Sent again. The new delivery shows in the log.");
                    onBack();
                  },
                  onError: (e) => toast.error(errorMessage(e, "Couldn't redeliver.")),
                })
              }
            >
              Redeliver
            </Button>
          </div>
          {d.data.error ? <Banner tone="danger">{d.data.error}</Banner> : null}
          <section className="flex flex-col gap-2">
            <h3 className="type-label font-semibold text-fg">Payload</h3>
            <pre className="max-h-96 overflow-auto rounded-md bg-sunken p-3 font-mono type-caption text-fg">{prettyJSON(d.data.payload)}</pre>
          </section>
          <section className="flex flex-col gap-2">
            <h3 className="type-label font-semibold text-fg">Response</h3>
            <pre className="max-h-64 overflow-auto whitespace-pre-wrap rounded-md bg-sunken p-3 font-mono type-caption text-fg [overflow-wrap:anywhere]">{d.data.response || "No response body."}</pre>
          </section>
        </>
      )}
    </div>
  );
}
