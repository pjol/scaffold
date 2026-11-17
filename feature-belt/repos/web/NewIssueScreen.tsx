"use client";

import {
  errorMessage,
  useCreateReposIssue,
  useReposLabels,
  useReposMilestones,
  useReposTemplates,
  type ReposMention,
  type ReposPerson,
  type ReposTemplate,
} from "@scaffoldapp/shared";
import { Button, Card, EmptyState, Field, Input, Skeleton, ToggleChip, toast } from "@scaffoldapp/ui-web";
import { CircleDot, FileText, Milestone as MilestoneIcon, Tag, UserPlus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useUrlState } from "@/lib/url-state";
import { GearButton, LabelBadge, LabelList, LabelPickerDialog, MARKDOWN_HINT, MarkdownEditor, MilestoneMenu, PeoplePicker, SideSection, encodeMentions } from "./issueParts";
import { Person } from "./parts";
import { repoHref, useRepoContext } from "./RepoLayout";

/** Open an issue: from a template in .github/ISSUE_TEMPLATE or blank; people who can edit add labels, assignees and a milestone. */
export function NewIssueScreen() {
  const repo = useRepoContext();
  const router = useRouter();
  const templates = useReposTemplates(repo.id, "issue");
  const [templateFile, setTemplateFile] = useUrlState("template", "");
  const [started, setStarted] = useState(false);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [mentions, setMentions] = useState<ReposMention[]>([]);
  const [labelIds, setLabelIds] = useState<string[]>([]);
  const [assignees, setAssignees] = useState<ReposPerson[]>([]);
  const [milestoneId, setMilestoneId] = useState<string | null>(null);
  const milestones = useReposMilestones(repo.id, "open", repo.can_push);
  const milestone = (milestones.data?.items ?? []).find((m) => m.id === milestoneId) ?? null;
  const [labelsOpen, setLabelsOpen] = useState(false);
  const [assigning, setAssigning] = useState(false);
  const labels = useReposLabels(repo.id, repo.can_push);
  const create = useCreateReposIssue(repo.id);
  const list = templates.data?.items ?? [];
  const tpl: ReposTemplate | undefined = list.find((t) => t.file === templateFile);
  const triage = repo.can_push;

  const choose = (t: ReposTemplate | null) => {
    setTemplateFile(t ? t.file : null);
    setTitle(t?.title ?? "");
    setBody(t?.body ?? "");
    setStarted(true);
  };

  if (repo.my_level === "view") {
    return (
      <Card>
        <EmptyState icon={<CircleDot />} title="You can read issues here" info="Opening issues needs comment access to the repository. Ask someone who manages it." />
      </Card>
    );
  }
  if (templates.isPending) return <Skeleton className="h-64 w-full" />;
  if (!started && !tpl && list.length > 0) {
    return (
      <div className="flex max-w-3xl flex-col gap-4">
        <h2 className="type-title text-fg">New issue</h2>
        <ul className="flex flex-col divide-y divide-border rounded-lg border border-border bg-surface">
          {list.map((t) => (
            <li key={t.file} className="flex flex-wrap items-center gap-3 px-4 py-3">
              <FileText className="size-5 shrink-0 text-fg-muted" aria-hidden />
              <span className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="type-body font-semibold text-fg">{t.name}</span>
                {t.about ? <span className="type-label text-fg-muted">{t.about}</span> : null}
              </span>
              <Button size="sm" onClick={() => choose(t)}>
                Get started
              </Button>
            </li>
          ))}
          <li className="flex items-center gap-3 px-4 py-3">
            <CircleDot className="size-5 shrink-0 text-fg-muted" aria-hidden />
            <span className="min-w-0 flex-1 type-body font-semibold text-fg">Blank issue</span>
            <Button size="sm" variant="outline" onClick={() => choose(null)}>
              Open a blank issue
            </Button>
          </li>
        </ul>
      </div>
    );
  }
  if (!started && tpl) {
    // Arrived with ?template=: fill once.
    setTitle(tpl.title);
    setBody(tpl.body);
    setStarted(true);
  }
  const chosenLabels = (labels.data?.items ?? []).filter((l) => labelIds.includes(l.id));
  return (
    <div className="flex flex-col gap-section lg:grid lg:grid-cols-[minmax(0,1fr)_16rem] lg:gap-10">
      <Card>
        <form
          className="flex flex-col gap-4"
          onSubmit={async (e) => {
            e.preventDefault();
            try {
              const is = await create.mutateAsync({
                title,
                body: encodeMentions(body.trim(), mentions),
                ...(tpl ? { template: tpl.file } : {}),
                ...(triage && labelIds.length ? { labels: labelIds } : {}),
                ...(triage && assignees.length ? { assignees: assignees.map((a) => a.id) } : {}),
                ...(triage && milestone ? { milestone_id: milestone.id } : {}),
              });
              toast.success(`Opened #${is.number}`);
              router.push(repoHref(repo.id, `issues/${is.number}`));
            } catch (err) {
              toast.error(errorMessage(err, "Couldn't open the issue."));
            }
          }}
        >
          <h2 className="type-title text-fg">{tpl ? tpl.name : "New issue"}</h2>
          {list.length > 0 ? (
            <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Template">
              <ToggleChip pressed={!tpl} onPressedChange={() => choose(null)}>
                Blank
              </ToggleChip>
              {list.map((t) => (
                <ToggleChip key={t.file} pressed={tpl?.file === t.file} onPressedChange={() => choose(t)}>
                  {t.name}
                </ToggleChip>
              ))}
            </div>
          ) : null}
          <Field label="Title">
            <Input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={256} autoFocus required />
          </Field>
          <MarkdownEditor repoId={repo.id} label="Description" value={body} onChange={setBody} mentions={mentions} onMentionsChange={setMentions} placeholder={MARKDOWN_HINT} minRows={8} />
          {tpl && tpl.labels.length ? (
            <p className="flex flex-wrap items-center gap-2 type-label text-fg-muted">
              The template adds
              {tpl.labels.map((n) => (
                <LabelBadge key={n} label={{ name: n, color: "neutral", description: "" }} />
              ))}
            </p>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button asChild variant="ghost">
              <Link href={repoHref(repo.id, "issues")}>Cancel</Link>
            </Button>
            <Button type="submit" loading={create.isPending} disabled={!title.trim()}>
              Open issue
            </Button>
          </div>
        </form>
      </Card>
      {triage ? (
        <aside className="flex flex-col gap-4">
          <SideSection title="Assignees" action={<GearButton label="Assign people" onClick={() => setAssigning(!assigning)} />}>
            {assigning ? (
              <PeoplePicker repoId={repo.id} value={assignees} onChange={setAssignees} title="People who can open the repository" />
            ) : assignees.length ? (
              <ul className="flex flex-col gap-2">
                {assignees.map((a) => (
                  <li key={a.id} className="type-label">
                    <Person user={a} />
                  </li>
                ))}
              </ul>
            ) : (
              <p className="flex items-center gap-2 type-label text-fg-muted">
                <UserPlus className="size-4" aria-hidden />
                No one yet.
              </p>
            )}
          </SideSection>
          <SideSection title="Labels" action={<GearButton label="Choose labels" onClick={() => setLabelsOpen(true)} />}>
            {chosenLabels.length ? (
              <LabelList labels={chosenLabels} />
            ) : (
              <p className="flex items-center gap-2 type-label text-fg-muted">
                <Tag className="size-4" aria-hidden />
                None yet.
              </p>
            )}
          </SideSection>
          <SideSection
            title="Milestone"
            action={
              <MilestoneMenu
                repoId={repo.id}
                value={milestoneId}
                onChange={setMilestoneId}
                trigger={<GearButton label="Choose a milestone" />}
              />
            }
          >
            <p className="flex items-center gap-2 type-label text-fg-muted">
              <MilestoneIcon className="size-4" aria-hidden />
              {milestone ? milestone.title : "No milestone."}
            </p>
          </SideSection>
          <LabelPickerDialog
            repoId={repo.id}
            open={labelsOpen}
            onOpenChange={setLabelsOpen}
            value={labelIds}
            onSave={async (ids) => {
              setLabelIds(ids);
            }}
          />
        </aside>
      ) : null}
    </div>
  );
}
