"use client";

import {
  errorMessage,
  useReposLabels,
  useReposMilestones,
  useReposPeople,
  useReposReaction,
  type ReposIssueReason,
  type ReposIssueState,
  type ReposLabel,
  type ReposMention,
  type ReposPerson,
  type ReposReaction,
  type ReposReactionEmoji,
  type ReposReactionSubject,
  type ReposUser,
} from "@scaffoldapp/shared";
import { Badge, Button, Checkbox, Dialog, IconButton, Input, Menu, MenuChoice, MenuSeparator, Skeleton, Tab, TabPanel, Tabs, TabsList, TextArea, Tooltip, cn, toast } from "@scaffoldapp/ui-web";
import { CircleCheck, CircleDot, CircleSlash, Search, Settings2, SmilePlus } from "lucide-react";
import { useMemo, useRef, useState, type ReactNode } from "react";
import { RepoMarkdown } from "./Markdown";
import { Person } from "./parts";

// Pieces the issue and pull request screens share: labels, state icons,
// reactions, pickers for labels, people and milestones, and a Markdown
// editor with @mentions written as <@id> tokens.

/** A label as a Badge in its token tone. */
export function LabelBadge({ label, size = "sm" }: { label: Pick<ReposLabel, "name" | "color" | "description">; size?: "sm" | "md" }) {
  const badge = (
    <Badge tone={label.color} size={size} className="max-w-48">
      <span className="truncate">{label.name}</span>
    </Badge>
  );
  return label.description ? <Tooltip content={label.description}>{badge}</Tooltip> : badge;
}

export function LabelList({ labels, className }: { labels: Pick<ReposLabel, "id" | "name" | "color" | "description">[]; className?: string }) {
  if (labels.length === 0) return null;
  return (
    <span className={cn("flex flex-wrap items-center gap-1", className)}>
      {labels.map((l) => (
        <LabelBadge key={l.id} label={l} />
      ))}
    </span>
  );
}

/** Open (success), closed as completed (accent) or not planned (neutral). */
export function IssueStateIcon({ state, reason, className = "size-5" }: { state: ReposIssueState; reason?: ReposIssueReason | null; className?: string }) {
  if (state === "open") return <CircleDot className={cn(className, "text-success-strong")} aria-label="Open" />;
  if (reason === "not_planned") return <CircleSlash className={cn(className, "text-fg-muted")} aria-label="Closed as not planned" />;
  return <CircleCheck className={cn(className, "text-accent-strong")} aria-label="Closed as completed" />;
}

export function issueStateBadge(state: ReposIssueState, reason?: ReposIssueReason | null) {
  if (state === "open")
    return (
      <Badge tone="success" size="md" icon={<CircleDot />}>
        Open
      </Badge>
    );
  if (reason === "not_planned")
    return (
      <Badge tone="neutral" size="md" icon={<CircleSlash />}>
        Not planned
      </Badge>
    );
  return (
    <Badge tone="accent" size="md" icon={<CircleCheck />}>
      Closed
    </Badge>
  );
}

// ---- reactions -------------------------------------------------------------------

export const REACTIONS: { emoji: ReposReactionEmoji; char: string; label: string }[] = [
  { emoji: "+1", char: "👍", label: "Thumbs up" },
  { emoji: "-1", char: "👎", label: "Thumbs down" },
  { emoji: "laugh", char: "😄", label: "Laugh" },
  { emoji: "hooray", char: "🎉", label: "Hooray" },
  { emoji: "confused", char: "😕", label: "Confused" },
  { emoji: "heart", char: "❤️", label: "Heart" },
  { emoji: "rocket", char: "🚀", label: "Rocket" },
  { emoji: "eyes", char: "👀", label: "Eyes" },
];

/** Reaction counts; toggling them needs comment access. */
export function Reactions({
  repoId,
  number,
  subject,
  reactions,
  canReact,
}: {
  repoId: string;
  number: number;
  subject: { type: ReposReactionSubject; id: string };
  reactions: ReposReaction[] | undefined;
  canReact: boolean;
}) {
  const react = useReposReaction(repoId);
  const list = reactions ?? [];
  const toggle = (emoji: ReposReactionEmoji, add: boolean) =>
    react.mutate({ type: subject.type, subjectId: subject.id, emoji, add, number }, { onError: (e) => toast.error(errorMessage(e, "Couldn't react.")) });
  if (list.length === 0 && !canReact) return null;
  return (
    <div className="flex flex-wrap items-center gap-1">
      {list.map((r) => {
        const def = REACTIONS.find((x) => x.emoji === r.emoji);
        return (
          <button
            key={r.emoji}
            type="button"
            disabled={!canReact || react.isPending}
            aria-pressed={r.me}
            aria-label={`${def?.label ?? r.emoji}: ${r.count}`}
            onClick={() => toggle(r.emoji, !r.me)}
            className={cn(
              "flex h-7 items-center gap-1 rounded-pill border px-2 type-caption tabular-nums transition-colors focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-default",
              r.me ? "border-primary bg-primary-soft text-primary-strong" : "border-border bg-surface text-fg-muted hover:bg-sunken",
            )}
          >
            <span aria-hidden>{def?.char ?? r.emoji}</span>
            {r.count}
          </button>
        );
      })}
      {canReact ? (
        <Menu align="start" trigger={<IconButton label="Add a reaction" icon={<SmilePlus />} variant="ghost" size="sm" />}>
          <div className="grid grid-cols-4 gap-1 p-1">
            {REACTIONS.map((x) => {
              const mine = list.some((r) => r.emoji === x.emoji && r.me);
              return (
                <button
                  key={x.emoji}
                  type="button"
                  aria-label={x.label}
                  aria-pressed={mine}
                  onClick={() => toggle(x.emoji, !mine)}
                  className={cn("flex size-10 items-center justify-center rounded-md type-title-sm hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring", mine && "bg-primary-soft")}
                >
                  <span aria-hidden>{x.char}</span>
                </button>
              );
            })}
          </div>
        </Menu>
      ) : null}
    </div>
  );
}

// ---- mentions --------------------------------------------------------------------

const MENTION_RE = /<@([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})>/g;

/** A body with <@id> tokens written as @Name, for editing. */
export function decodeMentions(body: string, mentions: ReposMention[] = []): { text: string; known: ReposMention[] } {
  const known: ReposMention[] = [];
  const text = body.replace(MENTION_RE, (raw, id: string) => {
    const m = mentions.find((x) => x.id === id);
    if (!m) return raw;
    if (!known.some((k) => k.id === m.id)) known.push(m);
    return `@${m.display_name}`;
  });
  return { text, known };
}

/** Turn @Name written through the picker back into <@id> tokens (longest names first). */
export function encodeMentions(text: string, mentions: ReposMention[]): string {
  let out = text;
  for (const m of [...mentions].sort((a, b) => b.display_name.length - a.display_name.length)) {
    const name = m.display_name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    out = out.replace(new RegExp(`(^|[^\\w@])@${name}(?![\\w])`, "g"), `$1<@${m.id}>`);
  }
  return out;
}

/** The word being typed after an @ at the caret, if any. */
function mentionQuery(text: string, caret: number): { start: number; q: string } | null {
  const before = text.slice(0, caret);
  const m = /(^|[\s(])@([\p{L}\p{N}._-]{0,30})$/u.exec(before);
  if (!m) return null;
  return { start: caret - m[2]!.length - 1, q: m[2]! };
}

export interface MarkdownEditorProps {
  repoId: string;
  value: string;
  onChange: (v: string) => void;
  /** People picked with @ (encode with encodeMentions before saving). */
  mentions: ReposMention[];
  onMentionsChange: (m: ReposMention[]) => void;
  label: string;
  placeholder?: string;
  minRows?: number;
  autoFocus?: boolean;
  gitRef?: string | undefined;
  /** Extra controls in the tab row (an "Insert suggestion" button). */
  toolbar?: ReactNode;
}

/** Write / Preview with @mention suggestions of people who can open the repository. */
export function MarkdownEditor({ repoId, value, onChange, mentions, onMentionsChange, label, placeholder, minRows = 4, autoFocus, gitRef, toolbar }: MarkdownEditorProps) {
  const [tab, setTab] = useState<"write" | "preview">("write");
  const ref = useRef<HTMLTextAreaElement | null>(null);
  const [query, setQuery] = useState<{ start: number; q: string } | null>(null);
  const [active, setActive] = useState(0);
  const people = useReposPeople(repoId, query?.q ?? "");
  const options = query ? (people.data?.items ?? []).slice(0, 6) : [];
  const pick = (p: ReposPerson) => {
    if (!query) return;
    const el = ref.current;
    const caret = el?.selectionStart ?? value.length;
    const insert = `@${p.display_name} `;
    const next = value.slice(0, query.start) + insert + value.slice(caret);
    onChange(next);
    if (!mentions.some((m) => m.id === p.id)) onMentionsChange([...mentions, { id: p.id, display_name: p.display_name }]);
    setQuery(null);
    requestAnimationFrame(() => {
      el?.focus();
      const at = query.start + insert.length;
      el?.setSelectionRange(at, at);
    });
  };
  const preview = useMemo(() => encodeMentions(value, mentions), [value, mentions]);
  return (
    <Tabs value={tab} onValueChange={(v) => setTab(v as "write" | "preview")}>
      <div className="flex flex-wrap items-center gap-2">
        <TabsList aria-label={label}>
          <Tab value="write">Write</Tab>
          <Tab value="preview">Preview</Tab>
        </TabsList>
        {toolbar ? <div className="ml-auto flex items-center gap-1">{toolbar}</div> : null}
      </div>
      <TabPanel value="write" className="relative pt-2">
        <TextArea
          ref={ref}
          aria-label={label}
          value={value}
          placeholder={placeholder}
          minRows={minRows}
          maxRows={20}
          autoFocus={autoFocus}
          onChange={(e) => {
            onChange(e.target.value);
            setQuery(mentionQuery(e.target.value, e.target.selectionStart ?? e.target.value.length));
            setActive(0);
          }}
          onKeyDown={(e) => {
            if (!query || options.length === 0) return;
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setActive((a) => (a + 1) % options.length);
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setActive((a) => (a - 1 + options.length) % options.length);
            } else if (e.key === "Enter" || e.key === "Tab") {
              e.preventDefault();
              pick(options[active]!);
            } else if (e.key === "Escape") {
              setQuery(null);
            }
          }}
          onBlur={() => setTimeout(() => setQuery(null), 150)}
        />
        {query && options.length > 0 ? (
          <ul role="listbox" aria-label="People to mention" className="absolute left-2 top-full z-(--app-z-popover) mt-1 w-64 rounded-md border border-border bg-surface-raised p-1 shadow-lg">
            {options.map((p, i) => (
              <li key={p.id} role="option" aria-selected={i === active}>
                <button
                  type="button"
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => pick(p)}
                  className={cn("flex min-h-9 w-full items-center gap-2 rounded-sm px-2 text-left type-label", i === active ? "bg-sunken" : "hover:bg-sunken")}
                >
                  <Person user={{ id: p.id, display_name: p.display_name, avatar_url: p.avatar_url }} />
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </TabPanel>
      <TabPanel value="preview" className="min-h-24 rounded-md border border-border px-3 py-2">
        {value.trim() ? <RepoMarkdown source={preview} repoId={repoId} gitRef={gitRef} dir="" mentions={mentions} /> : <p className="type-label text-fg-muted">Nothing to preview.</p>}
      </TabPanel>
    </Tabs>
  );
}

// ---- pickers ---------------------------------------------------------------------

/** A sidebar block: heading, a gear that opens the picker, and the value. */
export function SideSection({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2 border-b border-border pb-4 last:border-0">
      <div className="flex min-h-8 items-center justify-between gap-2">
        <h3 className="type-label font-semibold text-fg">{title}</h3>
        {action}
      </div>
      {children}
    </section>
  );
}

export function GearButton({ label, onClick }: { label: string; onClick?: () => void }) {
  return <IconButton label={label} icon={<Settings2 />} variant="ghost" size="sm" tooltip onClick={onClick} />;
}

/** Choose labels (checkboxes) in a dialog. */
export function LabelPickerDialog({
  repoId,
  open,
  onOpenChange,
  value,
  onSave,
  saving,
}: {
  repoId: string;
  open: boolean;
  onOpenChange: (o: boolean) => void;
  value: string[];
  onSave: (ids: string[]) => Promise<unknown>;
  saving?: boolean;
}) {
  const labels = useReposLabels(repoId, open);
  const [picked, setPicked] = useState<string[]>(value);
  const [q, setQ] = useState("");
  const shown = (labels.data?.items ?? []).filter((l) => !q || l.name.toLowerCase().includes(q.toLowerCase()));
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (o) setPicked(value);
        onOpenChange(o);
      }}
      title="Labels"
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            loading={saving}
            onClick={async () => {
              try {
                await onSave(picked);
                onOpenChange(false);
              } catch (e) {
                toast.error(errorMessage(e, "Couldn't change the labels."));
              }
            }}
          >
            Apply
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Input aria-label="Filter labels" placeholder="Filter labels" leadingIcon={<Search aria-hidden />} value={q} onChange={(e) => setQ(e.target.value)} />
        {labels.isPending ? (
          <Skeleton className="h-32 w-full" />
        ) : labels.isError ? (
          <p className="type-label text-danger-strong">{errorMessage(labels.error, "Couldn't load labels.")}</p>
        ) : shown.length === 0 ? (
          <p className="type-label text-fg-muted">No labels match.</p>
        ) : (
          <ul className="flex max-h-72 flex-col overflow-y-auto">
            {shown.map((l) => (
              <li key={l.id} className="flex min-h-control items-center px-1">
                <Checkbox
                  checked={picked.includes(l.id)}
                  onCheckedChange={(on) => setPicked(on ? [...picked, l.id] : picked.filter((x) => x !== l.id))}
                  label={<LabelBadge label={l} />}
                />
              </li>
            ))}
          </ul>
        )}
      </div>
    </Dialog>
  );
}

/** Choose a milestone (or none) from a menu. */
export function MilestoneMenu({ repoId, value, onChange, trigger }: { repoId: string; value: string | null; onChange: (id: string | null) => void; trigger: ReactNode }) {
  const ms = useReposMilestones(repoId, "open");
  return (
    <Menu trigger={trigger}>
      {ms.isPending ? (
        <div className="p-2">
          <Skeleton className="h-6 w-40" />
        </div>
      ) : (ms.data?.items ?? []).length === 0 ? (
        <p className="px-3 py-2 type-label text-fg-muted">No open milestones.</p>
      ) : (
        ms.data!.items.map((m) => (
          <MenuChoice key={m.id} checked={value === m.id} onSelect={() => onChange(m.id)} hint={m.due_on ? `Due ${m.due_on}` : undefined}>
            {m.title}
          </MenuChoice>
        ))
      )}
      {value ? (
        <>
          <MenuSeparator />
          <MenuChoice onSelect={() => onChange(null)}>No milestone</MenuChoice>
        </>
      ) : null}
    </Menu>
  );
}

/** People rows (assignees). */
export function PeopleList({ people, empty }: { people: ReposUser[]; empty: string }) {
  if (people.length === 0) return <p className="type-label text-fg-muted">{empty}</p>;
  return (
    <ul className="flex flex-col gap-2">
      {people.map((p) => (
        <li key={p.id} className="type-label">
          <Person user={p} />
        </li>
      ))}
    </ul>
  );
}

/** A mention-aware body shown as Markdown. */
export function Body({ source, repoId, gitRef, mentions }: { source: string; repoId: string; gitRef?: string | undefined; mentions?: ReposMention[] | undefined }) {
  return <RepoMarkdown source={source} repoId={repoId} gitRef={gitRef} dir="" mentions={mentions} />;
}

/** "Assignees" picker: people who can open the repository (≤ 10). */
export { ReviewerPicker as PeoplePicker } from "./CompareScreen";

/** Plain text used in placeholders and the composer's hint. */
export const MARKDOWN_HINT = "Markdown works here. Type @ to mention someone and # plus a number to link an issue or pull request.";
