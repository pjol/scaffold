"use client";

import { errorMessage, hasErrorCode, useReposCommitChanges, useReposRefs, type ReposChange, type ReposCommitResult } from "@scaffoldapp/shared";
import { Banner, Button, Checkbox, Dialog, Field, Input, SegmentedControl, TextArea, toast } from "@scaffoldapp/ui-web";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { repoHref, useRepoContext } from "./RepoLayout";

export interface CommitDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  /** The branch the changes were made on. */
  branch: string;
  /** The commit the changes started from ("" for an empty repository). */
  expectedHead: string;
  changes: ReposChange[];
  defaultMessage: string;
  confirmLabel?: string;
  tone?: "primary" | "danger";
  onDone: (res: ReposCommitResult) => void;
}

type Target = "branch" | "new";

/**
 * Commit a set of changes: to the current branch when it's allowed, or to a new branch with
 * a pull request offered. A branch that moved since the edit started is refused (409
 * head_moved) and the changes stay here to commit to a new branch.
 */
export function CommitDialog({ open, onOpenChange, title, branch, expectedHead, changes, defaultMessage, confirmLabel = "Commit changes", tone = "primary", onDone }: CommitDialogProps) {
  const repo = useRepoContext();
  const router = useRouter();
  const refs = useReposRefs(repo.id);
  const commit = useReposCommitChanges(repo.id);
  const current = refs.data?.items.find((r) => r.kind === "branch" && r.name === branch);
  // Protected branches take changes through pull requests (the first commit of an empty repository is the exception).
  const locked = !repo.empty && !!current?.protected;
  const [summary, setSummary] = useState(defaultMessage);
  const [description, setDescription] = useState("");
  const [target, setTarget] = useState<Target>("branch");
  const [newBranch, setNewBranch] = useState("");
  const [openPull, setOpenPull] = useState(true);
  const [moved, setMoved] = useState(false);
  const suggested = useMemo(() => suggestBranch(defaultMessage), [defaultMessage]);

  useEffect(() => {
    if (!open) return;
    setSummary(defaultMessage);
    setMoved(false);
    setNewBranch((b) => b || suggested);
  }, [open, defaultMessage, suggested]);
  useEffect(() => {
    if (locked) setTarget("new");
  }, [locked]);

  const effective: Target = locked || moved ? "new" : target;
  const submit = async () => {
    const message = description.trim() ? `${summary.trim()}\n\n${description.trim()}` : summary.trim();
    try {
      const res = await commit.mutateAsync({
        branch,
        expected_head: expectedHead,
        message,
        changes,
        ...(effective === "new" ? { new_branch: newBranch.trim() } : {}),
      });
      toast.success(effective === "new" ? `Committed to ${res.branch}` : "Committed");
      onOpenChange(false);
      if (effective === "new" && openPull) {
        router.push(repoHref(repo.id, "compare", { base: branch, head: res.branch, expand: 1 }));
        return;
      }
      onDone(res);
    } catch (e) {
      if (hasErrorCode(e, "head_moved")) {
        setMoved(true);
        return;
      }
      if (hasErrorCode(e, "branch_protected")) {
        setTarget("new");
      }
      toast.error(errorMessage(e, "Couldn't commit. Try again."));
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      size="md"
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant={tone} loading={commit.isPending} disabled={!summary.trim() || (effective === "new" && !newBranch.trim())} onClick={() => void submit()}>
            {effective === "new" && openPull ? `${confirmLabel} and compare` : confirmLabel}
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        {moved ? <Banner tone="warning">{branch} changed since you started. Your changes are kept: commit them to a new branch.</Banner> : null}
        <Field label="Commit message">
          <Input value={summary} onChange={(e) => setSummary(e.target.value)} maxLength={200} autoFocus />
        </Field>
        <Field label="Description">
          <TextArea value={description} onChange={(e) => setDescription(e.target.value)} minRows={2} placeholder="Why this change (optional)" />
        </Field>
        <Field
          label="Commit to"
          info={locked ? `${branch} is protected: changes reach it through pull requests.` : undefined}
        >
          <SegmentedControl<Target>
            aria-label="Commit to"
            value={effective}
            onValueChange={setTarget}
            options={[
              { value: "branch", label: branch, disabled: locked || moved },
              { value: "new", label: "A new branch" },
            ]}
          />
        </Field>
        {effective === "new" ? (
          <>
            <Field label="Branch name">
              <Input value={newBranch} onChange={(e) => setNewBranch(e.target.value)} className="font-mono" placeholder="feature/change" />
            </Field>
            <Checkbox checked={openPull} onCheckedChange={setOpenPull} label="Open a pull request next" />
          </>
        ) : null}
      </form>
    </Dialog>
  );
}

/** "Update README.md" → "update-readme-md" for a branch name suggestion. */
function suggestBranch(message: string): string {
  const s = message
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
    .replace(/-+$/, "");
  return s ? `edit/${s}` : "edit/change";
}
