"use client";

import {
  errorMessage,
  hasErrorCode,
  reposRelativeTime,
  reposShortSha,
  useAddReposIssueComment,
  useDeleteReposIssue,
  useDeleteReposIssueComment,
  useEditReposIssueComment,
  useReposIssue,
  useReposIssueTimeline,
  useSetReposIssueAssignees,
  useSetReposIssueLabels,
  useUpdateReposIssue,
  type ApiError,
  type ReposIssue,
  type ReposIssueComment,
  type ReposIssueEvent,
  type ReposMention,
  type ReposPerson,
  type ReposRepo,
} from "@scaffoldapp/shared";
import {
  Badge,
  Banner,
  Button,
  Card,
  ConfirmDialog,
  Dialog,
  ErrorState,
  IconButton,
  Input,
  Menu,
  MenuItem,
  MenuSeparator,
  Skeleton,
  SkeletonGroup,
  toast,
} from "@scaffoldapp/ui-web";
import {
  ChevronDown,
  CircleCheck,
  CircleDot,
  CircleSlash,
  Copy,
  GitCommitHorizontal,
  GitPullRequest,
  Link2,
  Lock,
  LockOpen,
  Milestone,
  MoreHorizontal,
  Pencil,
  Pin,
  PinOff,
  Tag,
  Trash2,
  UserPlus,
  X,
} from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  Body,
  GearButton,
  LabelBadge,
  LabelList,
  LabelPickerDialog,
  MARKDOWN_HINT,
  MarkdownEditor,
  MilestoneMenu,
  PeopleList,
  PeoplePicker,
  Reactions,
  SideSection,
  decodeMentions,
  encodeMentions,
  issueStateBadge,
} from "./issueParts";
import { PullStateIcon } from "./PullsScreen";
import { Person } from "./parts";
import { copyText, repoHref, useRepoContext } from "./RepoLayout";

/** One issue: description, conversation, triage in the sidebar, close and reopen. */
export function IssueScreen({ number }: { number: number }) {
  const repo = useRepoContext();
  const router = useRouter();
  const issue = useReposIssue(repo.id, number);
  const isPull = issue.isError && hasErrorCode(issue.error, "is_pull_request");
  useEffect(() => {
    if (isPull) router.replace(repoHref(repo.id, `pulls/${number}`));
  }, [isPull, router, repo.id, number]);

  if (issue.isPending || isPull) {
    return (
      <SkeletonGroup label="Loading issue" className="flex flex-col gap-4">
        <Skeleton className="h-10 w-2/3" />
        <Skeleton className="h-5 w-1/2" />
        <Skeleton className="h-64 w-full" />
      </SkeletonGroup>
    );
  }
  if (issue.isError) {
    return (
      <Card>
        <ErrorState
          title={(issue.error as ApiError).status === 404 ? "Issue not found" : "Couldn't load this issue"}
          info={errorMessage(issue.error, "Try again.")}
          onRetry={() => void issue.refetch()}
        />
      </Card>
    );
  }
  const is = issue.data;
  return (
    <div className="flex flex-col gap-6">
      <IssueHeader repo={repo} issue={is} />
      <div className="flex flex-col gap-section lg:grid lg:grid-cols-[minmax(0,1fr)_16rem] lg:gap-10">
        <Conversation repo={repo} issue={is} />
        <Sidebar repo={repo} issue={is} />
      </div>
    </div>
  );
}

function IssueHeader({ repo, issue: is }: { repo: ReposRepo; issue: ReposIssue }) {
  const router = useRouter();
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(is.title);
  const [deleting, setDeleting] = useState(false);
  const update = useUpdateReposIssue(repo.id, is.number);
  const del = useDeleteReposIssue(repo.id, is.number);
  const change = (body: Parameters<typeof update.mutate>[0], ok: string) =>
    update.mutate(body, { onSuccess: () => toast.success(ok), onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) });
  return (
    <div className="flex flex-col gap-3">
      {editing ? (
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={async (e) => {
            e.preventDefault();
            try {
              await update.mutateAsync({ title });
              setEditing(false);
            } catch (err) {
              toast.error(errorMessage(err, "Couldn't rename it."));
            }
          }}
        >
          <Input aria-label="Title" value={title} onChange={(e) => setTitle(e.target.value)} autoFocus wrapperClassName="min-w-0 flex-1" maxLength={256} />
          <Button type="submit" size="sm" loading={update.isPending}>
            Save
          </Button>
          <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>
            Cancel
          </Button>
        </form>
      ) : (
        <div className="flex items-start gap-2">
          <h2 className="min-w-0 flex-1 type-headline text-fg">
            {is.title} <span className="font-normal text-fg-muted">#{is.number}</span>
          </h2>
          {is.can_edit ? <IconButton label="Edit title" icon={<Pencil />} variant="ghost" size="sm" tooltip onClick={() => (setTitle(is.title), setEditing(true))} /> : null}
          <Menu trigger={<IconButton label="Issue actions" icon={<MoreHorizontal />} variant="ghost" size="sm" />}>
            <MenuItem icon={<Link2 />} onSelect={() => void copyText(`${window.location.origin}${repoHref(repo.id, `issues/${is.number}`)}`, "Link copied")}>
              Copy link
            </MenuItem>
            <MenuItem icon={<Copy />} onSelect={() => void copyText(`#${is.number}`, "Reference copied")}>
              Copy #{is.number}
            </MenuItem>
            {is.can_triage ? (
              <>
                <MenuSeparator />
                <MenuItem icon={is.pinned ? <PinOff /> : <Pin />} onSelect={() => change({ pinned: !is.pinned }, is.pinned ? "Unpinned" : "Pinned to the issues list")}>
                  {is.pinned ? "Unpin" : "Pin"}
                </MenuItem>
                <MenuItem icon={is.locked ? <LockOpen /> : <Lock />} onSelect={() => change({ locked: !is.locked }, is.locked ? "Conversation open again" : "Conversation locked")}>
                  {is.locked ? "Open the conversation again" : "Lock conversation"}
                </MenuItem>
              </>
            ) : null}
            {is.can_delete ? (
              <>
                <MenuSeparator />
                <MenuItem icon={<Trash2 />} destructive onSelect={() => setDeleting(true)}>
                  Delete issue
                </MenuItem>
              </>
            ) : null}
          </Menu>
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2 type-label text-fg-muted">
        {issueStateBadge(is.state, is.state_reason)}
        {is.locked ? (
          <Badge tone="neutral" icon={<Lock />}>
            Locked
          </Badge>
        ) : null}
        {is.pinned ? (
          <Badge tone="neutral" icon={<Pin />}>
            Pinned
          </Badge>
        ) : null}
        <span>
          <span className="font-semibold text-fg">{is.author?.display_name || "Deleted user"}</span> opened this {reposRelativeTime(is.created_at)} · {is.comment_count}{" "}
          {is.comment_count === 1 ? "comment" : "comments"}
        </span>
      </div>
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={`Delete issue #${is.number}?`}
        tone="danger"
        confirmLabel="Delete issue"
        onConfirm={async () => {
          await del.mutateAsync();
          toast.success(`Deleted #${is.number}`);
          router.push(repoHref(repo.id, "issues"));
        }}
        errorText={(e) => errorMessage(e, "Couldn't delete it.")}
      >
        The issue, its comments and reactions go for good. Its number isn't reused.
      </ConfirmDialog>
    </div>
  );
}

// ---- conversation ------------------------------------------------------------------

function Conversation({ repo, issue: is }: { repo: ReposRepo; issue: ReposIssue }) {
  const timeline = useReposIssueTimeline(repo.id, is.number);
  const mentions = useMemo(() => [...(is.mentions ?? []), ...(timeline.data?.mentions ?? [])], [is.mentions, timeline.data]);
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Description repo={repo} issue={is} />
      {timeline.isPending ? (
        <SkeletonGroup label="Loading conversation" className="flex flex-col gap-3">
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </SkeletonGroup>
      ) : timeline.isError ? (
        <Card>
          <ErrorState info={errorMessage(timeline.error, "Try again.")} onRetry={() => void timeline.refetch()} />
        </Card>
      ) : (
        <ol className="flex flex-col gap-3">
          {timeline.data.items.map((item) => (
            <li key={item.type === "comment" ? item.comment.id : item.event.id}>
              {item.type === "comment" ? (
                <CommentCard repo={repo} issue={is} comment={item.comment} mentions={mentions} />
              ) : (
                <EventLine repo={repo} event={item.event} mentions={mentions} />
              )}
            </li>
          ))}
        </ol>
      )}
      <Composer repo={repo} issue={is} />
    </div>
  );
}

function Description({ repo, issue: is }: { repo: ReposRepo; issue: ReposIssue }) {
  const [editing, setEditing] = useState(false);
  const [body, setBody] = useState("");
  const [mentions, setMentions] = useState<ReposMention[]>([]);
  const update = useUpdateReposIssue(repo.id, is.number);
  return (
    <Card padding="none">
      <div className="flex min-h-12 items-center gap-2 border-b border-border px-4 type-label">
        <Person user={is.author} />
        <span className="text-fg-muted">opened {reposRelativeTime(is.created_at)}</span>
        {is.can_edit && !editing ? (
          <IconButton
            label="Edit description"
            icon={<Pencil />}
            variant="ghost"
            size="sm"
            className="ml-auto"
            onClick={() => {
              const d = decodeMentions(is.body, is.mentions);
              setBody(d.text);
              setMentions(d.known);
              setEditing(true);
            }}
          />
        ) : null}
      </div>
      <div className="flex flex-col gap-3 p-4">
        {editing ? (
          <div className="flex flex-col gap-2">
            <MarkdownEditor repoId={repo.id} label="Description" value={body} onChange={setBody} mentions={mentions} onMentionsChange={setMentions} autoFocus minRows={6} />
            <div className="flex justify-end gap-2">
              <Button variant="ghost" size="sm" onClick={() => setEditing(false)}>
                Cancel
              </Button>
              <Button
                size="sm"
                loading={update.isPending}
                onClick={async () => {
                  try {
                    await update.mutateAsync({ body: encodeMentions(body, mentions) });
                    setEditing(false);
                  } catch (e) {
                    toast.error(errorMessage(e, "Couldn't save."));
                  }
                }}
              >
                Save
              </Button>
            </div>
          </div>
        ) : is.body ? (
          <Body source={is.body} repoId={repo.id} mentions={is.mentions} />
        ) : (
          <p className="type-body text-fg-muted">No description.</p>
        )}
        <Reactions repoId={repo.id} number={is.number} subject={{ type: "issue", id: is.id }} reactions={is.reactions} canReact={repo.my_level !== "view"} />
      </div>
    </Card>
  );
}

function CommentCard({ repo, issue, comment: c, mentions }: { repo: ReposRepo; issue: ReposIssue; comment: ReposIssueComment; mentions: ReposMention[] }) {
  const [editing, setEditing] = useState(false);
  const [body, setBody] = useState("");
  const [picked, setPicked] = useState<ReposMention[]>([]);
  const [deleting, setDeleting] = useState(false);
  const edit = useEditReposIssueComment(repo.id, issue.number);
  const del = useDeleteReposIssueComment(repo.id, issue.number);
  return (
    <div className="flex flex-col rounded-lg border border-border bg-surface">
      <div className="flex min-h-10 items-center gap-2 border-b border-border px-4 type-label">
        <Person user={c.author} />
        <span className="text-fg-muted">{reposRelativeTime(c.created_at)}</span>
        {c.edited_at ? <span className="type-caption text-fg-subtle">edited</span> : null}
        {c.can_edit || c.can_delete ? (
          <span className="ml-auto">
            <Menu trigger={<IconButton label="Comment actions" icon={<MoreHorizontal />} variant="ghost" size="sm" />}>
              {c.can_edit ? (
                <MenuItem
                  icon={<Pencil />}
                  onSelect={() => {
                    const d = decodeMentions(c.body, mentions);
                    setBody(d.text);
                    setPicked(d.known);
                    setEditing(true);
                  }}
                >
                  Edit
                </MenuItem>
              ) : null}
              {c.can_delete ? (
                <MenuItem icon={<Trash2 />} destructive onSelect={() => setDeleting(true)}>
                  Delete
                </MenuItem>
              ) : null}
            </Menu>
          </span>
        ) : null}
      </div>
      <div className="flex flex-col gap-3 px-4 py-3">
        {c.deleted ? (
          <p className="type-label italic text-fg-muted">This comment was deleted.</p>
        ) : editing ? (
          <div className="flex flex-col gap-2">
            <MarkdownEditor repoId={repo.id} label="Comment" value={body} onChange={setBody} mentions={picked} onMentionsChange={setPicked} autoFocus minRows={3} />
            <div className="flex justify-end gap-2">
              <Button size="sm" variant="ghost" onClick={() => setEditing(false)}>
                Cancel
              </Button>
              <Button
                size="sm"
                loading={edit.isPending}
                onClick={async () => {
                  try {
                    await edit.mutateAsync({ commentId: c.id, body: encodeMentions(body, picked) });
                    setEditing(false);
                  } catch (e) {
                    toast.error(errorMessage(e, "Couldn't save."));
                  }
                }}
              >
                Save
              </Button>
            </div>
          </div>
        ) : (
          <>
            <Body source={c.body} repoId={repo.id} mentions={mentions} />
            <Reactions repoId={repo.id} number={issue.number} subject={{ type: "issue_comment", id: c.id }} reactions={c.reactions} canReact={issue.can_comment} />
          </>
        )}
      </div>
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title="Delete this comment?"
        tone="danger"
        confirmLabel="Delete"
        onConfirm={() => del.mutateAsync(c.id)}
        errorText={(e) => errorMessage(e, "Couldn't delete it.")}
      >
        It can't be undone.
      </ConfirmDialog>
    </div>
  );
}

interface IssueEventData {
  name?: string;
  color?: string;
  title?: string;
  from?: string;
  to?: string;
  reason?: string;
  pull?: number;
  commit?: string;
  message?: string;
  user_id?: string;
  source?: "issue" | "pull" | "commit";
  number?: number;
  sha?: string;
  closes?: boolean;
}

function EventLine({ repo, event: e, mentions }: { repo: ReposRepo; event: ReposIssueEvent; mentions: ReposMention[] }) {
  const who = e.actor?.display_name || (e.actor ? "Deleted user" : "Someone");
  const d = e.data as IssueEventData;
  const person = d.user_id ? (mentions.find((m) => m.id === d.user_id)?.display_name ?? "someone") : "";
  let icon: ReactNode = <CircleDot />;
  let text: ReactNode;
  switch (e.kind) {
    case "closed":
      icon = d.reason === "not_planned" ? <CircleSlash /> : <CircleCheck />;
      text =
        d.pull !== undefined ? (
          <>
            closed this as completed in{" "}
            <Link href={repoHref(repo.id, `pulls/${d.pull}`)} className="font-semibold text-fg hover:underline">
              #{d.pull}
            </Link>
          </>
        ) : d.commit ? (
          <>
            closed this as completed in{" "}
            <Link href={repoHref(repo.id, `commits/${d.commit}`)} className="font-mono hover:underline">
              {reposShortSha(d.commit)}
            </Link>
          </>
        ) : d.reason === "not_planned" ? (
          "closed this as not planned"
        ) : (
          "closed this as completed"
        );
      break;
    case "reopened":
      text = "reopened this";
      break;
    case "renamed":
      icon = <Pencil />;
      text = (
        <>
          changed the title from <del>{d.from}</del> to {d.to}
        </>
      );
      break;
    case "labeled":
    case "unlabeled":
      icon = <Tag />;
      text = (
        <>
          {e.kind === "labeled" ? "added" : "removed"} <LabelBadge label={{ name: d.name ?? "", color: (d.color as never) ?? "neutral", description: "" }} />
        </>
      );
      break;
    case "assigned":
    case "unassigned":
      icon = <UserPlus />;
      text = e.actor && d.user_id === e.actor.id ? (e.kind === "assigned" ? "self-assigned this" : "removed their assignment") : `${e.kind === "assigned" ? "assigned" : "unassigned"} ${person}`;
      break;
    case "milestoned":
    case "demilestoned":
      icon = <Milestone />;
      text = `${e.kind === "milestoned" ? "added this to" : "removed this from"} the ${d.title ?? ""} milestone`;
      break;
    case "locked":
      icon = <Lock />;
      text = "locked the conversation";
      break;
    case "unlocked":
      icon = <LockOpen />;
      text = "opened the conversation again";
      break;
    case "pinned":
      icon = <Pin />;
      text = "pinned this issue";
      break;
    case "unpinned":
      icon = <PinOff />;
      text = "unpinned this issue";
      break;
    case "referenced":
      icon = d.source === "commit" ? <GitCommitHorizontal /> : d.source === "pull" ? <GitPullRequest /> : <Link2 />;
      text =
        d.source === "commit" ? (
          <>
            mentioned this in{" "}
            <Link href={repoHref(repo.id, `commits/${d.sha}`)} className="font-mono hover:underline">
              {reposShortSha(d.sha ?? "")}
            </Link>{" "}
            {d.message}
          </>
        ) : (
          <>
            {d.closes ? "linked a pull request that will close this:" : "mentioned this in"}{" "}
            <Link href={repoHref(repo.id, `${d.source === "pull" ? "pulls" : "issues"}/${d.number}`)} className="font-semibold text-fg hover:underline">
              {d.title} #{d.number}
            </Link>
          </>
        );
      break;
  }
  return (
    <div className="flex items-center gap-2 py-1 pl-1 type-label text-fg-muted">
      <span className="flex size-6 shrink-0 items-center justify-center rounded-pill bg-sunken [&_svg]:size-3.5">{icon}</span>
      <span className="min-w-0">
        <span className="font-semibold text-fg">{who} </span>
        {text} <span className="type-caption">{reposRelativeTime(e.created_at)}</span>
      </span>
    </div>
  );
}

function Composer({ repo, issue: is }: { repo: ReposRepo; issue: ReposIssue }) {
  const [body, setBody] = useState("");
  const [mentions, setMentions] = useState<ReposMention[]>([]);
  const add = useAddReposIssueComment(repo.id, is.number);
  const update = useUpdateReposIssue(repo.id, is.number);
  if (!is.can_comment) {
    return is.locked ? (
      <Banner tone="neutral" icon={<Lock />}>
        This conversation is locked. People who can edit the repository can still comment.
      </Banner>
    ) : null;
  }
  const comment = async () => {
    await add.mutateAsync(encodeMentions(body, mentions));
    setBody("");
    setMentions([]);
  };
  const close = async (reason: "completed" | "not_planned" | null) => {
    try {
      if (body.trim()) await comment();
      await update.mutateAsync(reason ? { state: "closed", state_reason: reason } : { state: "open" });
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't change it."));
    }
  };
  return (
    <Card>
      <form
        className="flex flex-col gap-3"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            await comment();
          } catch (err) {
            toast.error(errorMessage(err, "Couldn't comment."));
          }
        }}
      >
        <MarkdownEditor repoId={repo.id} label="Add a comment" value={body} onChange={setBody} mentions={mentions} onMentionsChange={setMentions} placeholder={MARKDOWN_HINT} minRows={3} />
        <div className="flex flex-wrap justify-end gap-2">
          {is.can_edit ? (
            is.state === "open" ? (
              <div className="flex">
                <Button type="button" variant="outline" loading={update.isPending} leftIcon={<CircleCheck />} className="rounded-r-none" onClick={() => void close("completed")}>
                  {body.trim() ? "Close with comment" : "Close issue"}
                </Button>
                <Menu trigger={<IconButton label="More ways to close" icon={<ChevronDown />} variant="outline" className="rounded-l-none border-l-0" />}>
                  <MenuItem icon={<CircleCheck />} onSelect={() => void close("completed")}>
                    Close as completed
                  </MenuItem>
                  <MenuItem icon={<CircleSlash />} onSelect={() => void close("not_planned")}>
                    Close as not planned
                  </MenuItem>
                </Menu>
              </div>
            ) : (
              <Button type="button" variant="outline" loading={update.isPending} leftIcon={<CircleDot />} onClick={() => void close(null)}>
                Reopen issue
              </Button>
            )
          ) : null}
          <Button type="submit" loading={add.isPending} disabled={!body.trim()}>
            Comment
          </Button>
        </div>
      </form>
    </Card>
  );
}

// ---- sidebar -----------------------------------------------------------------------

function Sidebar({ repo, issue: is }: { repo: ReposRepo; issue: ReposIssue }) {
  const [labelsOpen, setLabelsOpen] = useState(false);
  const [assigning, setAssigning] = useState(false);
  const [picked, setPicked] = useState<ReposPerson[]>([]);
  const setLabels = useSetReposIssueLabels(repo.id, is.number);
  const setAssignees = useSetReposIssueAssignees(repo.id, is.number);
  const update = useUpdateReposIssue(repo.id, is.number);
  return (
    <aside className="flex flex-col gap-4">
      <SideSection
        title="Assignees"
        action={
          is.can_triage ? (
            <GearButton
              label="Assign people"
              onClick={() => {
                setPicked(is.assignees.map((a) => ({ id: a.id, display_name: a.display_name, avatar_url: a.avatar_url, level: "view" })));
                setAssigning(true);
              }}
            />
          ) : undefined
        }
      >
        <PeopleList people={is.assignees} empty="No one yet." />
      </SideSection>
      <SideSection title="Labels" action={is.can_triage ? <GearButton label="Change labels" onClick={() => setLabelsOpen(true)} /> : undefined}>
        {is.labels.length ? <LabelList labels={is.labels} /> : <p className="type-label text-fg-muted">None yet.</p>}
      </SideSection>
      <SideSection
        title="Milestone"
        action={
          is.can_triage ? (
            <MilestoneMenu
              repoId={repo.id}
              value={is.milestone?.id ?? null}
              onChange={(id) => update.mutate({ milestone_id: id ?? "" }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) })}
              trigger={<IconButton label="Choose a milestone" icon={<Milestone />} variant="ghost" size="sm" />}
            />
          ) : undefined
        }
      >
        {is.milestone ? (
          <Link href={repoHref(repo.id, "issues", { q: `is:open milestone:"${is.milestone.title}"` })} className="type-label font-semibold text-fg hover:underline">
            {is.milestone.title}
            {is.milestone.due_on ? <span className="ml-2 font-normal text-fg-muted">due {is.milestone.due_on}</span> : null}
          </Link>
        ) : (
          <p className="type-label text-fg-muted">No milestone.</p>
        )}
      </SideSection>
      <SideSection title="Development">
        {(is.pulls ?? []).length === 0 ? (
          <p className="type-label text-fg-muted">No pull requests link here. Write “Fixes #{is.number}” in one to close this when it merges.</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {is.pulls!.map((p) => (
              <li key={p.number} className="flex items-start gap-2 type-label">
                <PullStateIcon state={p.state} className="mt-0.5 size-4 shrink-0" />
                <Link href={repoHref(repo.id, `pulls/${p.number}`)} className="min-w-0 flex-1 text-fg hover:underline">
                  {p.title} <span className="text-fg-muted">#{p.number}</span>
                </Link>
                {p.closes ? <Badge tone="info">Closes</Badge> : null}
              </li>
            ))}
          </ul>
        )}
      </SideSection>
      <LabelPickerDialog repoId={repo.id} open={labelsOpen} onOpenChange={setLabelsOpen} value={is.labels.map((l) => l.id)} onSave={(ids) => setLabels.mutateAsync(ids)} saving={setLabels.isPending} />
      <Dialog
        open={assigning}
        onOpenChange={setAssigning}
        title="Assignees"
        footer={
          <>
            <Button variant="ghost" onClick={() => setAssigning(false)}>
              Cancel
            </Button>
            <Button
              loading={setAssignees.isPending}
              onClick={async () => {
                try {
                  await setAssignees.mutateAsync(picked.map((p) => p.id));
                  setAssigning(false);
                } catch (e) {
                  toast.error(errorMessage(e, "Couldn't assign them."));
                }
              }}
            >
              Save
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-3">
          {picked.length ? (
            <ul className="flex flex-wrap gap-2">
              {picked.map((p) => (
                <li key={p.id} className="flex items-center gap-1 rounded-pill bg-sunken py-0.5 pl-1 pr-0.5 type-label">
                  <Person user={p} />
                  <IconButton label={`Unassign ${p.display_name}`} icon={<X />} variant="ghost" size="sm" onClick={() => setPicked(picked.filter((x) => x.id !== p.id))} />
                </li>
              ))}
            </ul>
          ) : null}
          <PeoplePicker repoId={repo.id} value={picked} onChange={setPicked} title="People who can open the repository" />
        </div>
      </Dialog>
    </aside>
  );
}
