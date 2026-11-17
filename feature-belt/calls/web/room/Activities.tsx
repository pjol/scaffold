"use client";

import {
  errorMessage,
  useAskCallsQuestion,
  useCallsRoomState,
  useCreateCallsPoll,
  useDeleteCallsPoll,
  useDeleteCallsQuestion,
  useModerateCallsQuestion,
  useUpdateCallsPoll,
  useVoteCallsPoll,
  useVoteCallsQuestion,
  type CallsPoll,
  type CallsQuestion,
  type CallsRoomState,
} from "@scaffoldapp/shared";
import {
  Badge,
  Button,
  Checkbox,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  Menu,
  MenuItem,
  SegmentedControl,
  Skeleton,
  SkeletonGroup,
  Switch,
  Tab,
  TabsList,
  TabPanel,
  Tabs,
  TextArea,
  cn,
  toast,
} from "@scaffoldapp/ui-web";
import { BarChart3, Check, CircleHelp, EyeOff, MoreVertical, Plus, RotateCcw, ThumbsUp, Trash2, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import { BreakoutsHost } from "./BreakoutsHost";
import { inBreakoutRoom, useCallSession, useRoomTarget, type CallActivity } from "./session";

/** Polls, questions and (hosts, main room) breakout rooms, in one panel. */
export function ActivitiesPanel({ isHost, tab, onTab }: { isHost: boolean; tab: CallActivity; onTab: (t: CallActivity) => void }) {
  const s = useCallSession();
  const target = useRoomTarget();
  const state = useCallsRoomState(target);
  const showBreakouts = isHost && !s.guest && !inBreakoutRoom(s);
  const pending = state.data?.questions.filter((q) => q.status === "pending").length ?? 0;
  return (
    <Tabs value={tab} onValueChange={(v) => onTab(v as CallActivity)} className="flex h-full min-h-0 flex-col gap-4">
      <TabsList aria-label="Activities">
        <Tab value="polls">Polls</Tab>
        <Tab value="questions" {...(isHost && pending > 0 ? { count: pending } : {})}>
          Q&amp;A
        </Tab>
        {showBreakouts && <Tab value="breakouts">Breakout rooms</Tab>}
      </TabsList>
      <TabPanel value="polls" className="min-h-0 flex-1">
        <RoomQuery state={state}>{(d) => <PollsTab data={d} isHost={isHost} />}</RoomQuery>
      </TabPanel>
      <TabPanel value="questions" className="min-h-0 flex-1">
        <RoomQuery state={state}>{(d) => <QuestionsTab data={d} isHost={isHost} />}</RoomQuery>
      </TabPanel>
      {showBreakouts && (
        <TabPanel value="breakouts" className="min-h-0 flex-1">
          <BreakoutsHost />
        </TabPanel>
      )}
    </Tabs>
  );
}

function RoomQuery({ state, children }: { state: ReturnType<typeof useCallsRoomState>; children: (d: CallsRoomState) => React.ReactNode }) {
  if (state.isPending) {
    return (
      <SkeletonGroup label="Loading" className="flex flex-col gap-3">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
      </SkeletonGroup>
    );
  }
  if (state.isError) return <ErrorState compact title="Couldn't load this" info={state.error.message} onRetry={() => void state.refetch()} retrying={state.isFetching} />;
  return <>{children(state.data)}</>;
}

// ───────────────────────────────────────────────────────────── polls ──

function PollsTab({ data, isHost }: { data: CallsRoomState; isHost: boolean }) {
  const [creating, setCreating] = useState(false);
  const polls = [...data.polls].reverse(); // newest first
  return (
    <div className="flex flex-col gap-4">
      {isHost && (creating ? <PollForm onDone={() => setCreating(false)} /> : <Button variant="secondary" leftIcon={<Plus />} onClick={() => setCreating(true)}>New poll</Button>)}
      {polls.length === 0 ? (
        !creating && <EmptyState compact icon={<BarChart3 />} title={isHost ? "No polls yet" : "No polls right now"} />
      ) : (
        <ul className="flex flex-col gap-3">
          {polls.map((p) => (
            <PollCard key={p.id} poll={p} isHost={isHost} />
          ))}
        </ul>
      )}
    </div>
  );
}

function PollForm({ onDone }: { onDone: () => void }) {
  const s = useCallSession();
  const create = useCreateCallsPoll(s.meetingId);
  const [question, setQuestion] = useState("");
  const [options, setOptions] = useState(["", ""]);
  const [multiple, setMultiple] = useState(false);
  const [anonymous, setAnonymous] = useState(true);
  const filled = options.map((o) => o.trim()).filter(Boolean);
  const valid = question.trim().length > 0 && filled.length >= 2 && new Set(filled).size === filled.length;
  const submit = (open: boolean) =>
    create.mutate(
      { question, options: filled, multiple, anonymous, open },
      { onSuccess: () => (toast.success(open ? "Poll launched" : "Poll saved"), onDone()), onError: (e) => toast.error(errorMessage(e, "Couldn't save the poll.")) },
    );
  return (
    <form className="flex flex-col gap-4 rounded-lg border border-border p-4" onSubmit={(e: FormEvent) => (e.preventDefault(), valid && submit(true))}>
      <Field label="Question">
        <Input value={question} onChange={(e) => setQuestion(e.target.value)} maxLength={300} autoFocus />
      </Field>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 type-label font-semibold text-fg">Options</legend>
        {options.map((o, i) => (
          <div key={i} className="flex items-center gap-2">
            <Input
              value={o}
              aria-label={`Option ${i + 1}`}
              maxLength={100}
              onChange={(e) => setOptions((list) => list.map((x, j) => (j === i ? e.target.value : x)))}
              className="flex-1"
            />
            {options.length > 2 && <IconButton label={`Remove option ${i + 1}`} icon={<X />} variant="ghost" size="sm" onClick={() => setOptions((list) => list.filter((_, j) => j !== i))} />}
          </div>
        ))}
        {options.length < 10 && (
          <Button type="button" variant="ghost" size="sm" leftIcon={<Plus />} className="self-start" onClick={() => setOptions((list) => [...list, ""])}>
            Add option
          </Button>
        )}
      </fieldset>
      <div className="flex flex-col gap-3">
        <Switch checked={multiple} onCheckedChange={setMultiple} label="People can pick more than one" />
        <Switch checked={anonymous} onCheckedChange={setAnonymous} label="Anonymous" info="Hosts see totals, not who voted for what." />
      </div>
      <div className="flex flex-wrap justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onDone}>
          Cancel
        </Button>
        <Button type="button" variant="secondary" disabled={!valid} loading={create.isPending && create.variables?.open === false} onClick={() => submit(false)}>
          Save for later
        </Button>
        <Button type="submit" disabled={!valid} loading={create.isPending && create.variables?.open === true}>
          Launch
        </Button>
      </div>
    </form>
  );
}

const POLL_STATUS: Record<CallsPoll["status"], { label: string; tone: "neutral" | "success" | "primary" }> = {
  draft: { label: "Draft", tone: "neutral" },
  open: { label: "Live", tone: "success" },
  closed: { label: "Ended", tone: "primary" },
};

function PollCard({ poll: p, isHost }: { poll: CallsPoll; isHost: boolean }) {
  const s = useCallSession();
  const target = useRoomTarget();
  const vote = useVoteCallsPoll(target);
  const update = useUpdateCallsPoll(s.meetingId);
  const del = useDeleteCallsPoll(s.meetingId);
  const [picked, setPicked] = useState<number[]>(p.my_choices);
  const [changing, setChanging] = useState(false);
  const voted = p.my_choices.length > 0;
  const canVote = p.status === "open" && (!voted || changing);
  const showResults = !!p.counts && (isHost || p.results_shared);
  const fail = (msg: string) => (e: unknown) => toast.error(errorMessage(e, msg));
  const total = p.counts?.reduce((a, b) => a + b, 0) ?? 0;

  return (
    <li className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4">
      <div className="flex items-start gap-2">
        <p className="min-w-0 flex-1 type-body font-semibold text-fg">{p.question}</p>
        <Badge tone={POLL_STATUS[p.status].tone}>{POLL_STATUS[p.status].label}</Badge>
        {isHost && (
          <Menu trigger={<IconButton label="Poll options" icon={<MoreVertical />} variant="ghost" size="sm" />}>
            {p.status === "closed" && (
              <MenuItem icon={p.results_shared ? <EyeOff /> : <BarChart3 />} onSelect={() => update.mutate({ pollId: p.id, results_shared: !p.results_shared }, { onError: fail("Couldn't change that.") })}>
                {p.results_shared ? "Hide results" : "Share results"}
              </MenuItem>
            )}
            <MenuItem icon={<Trash2 />} destructive onSelect={() => del.mutate(p.id, { onError: fail("Couldn't delete the poll.") })}>
              Delete poll
            </MenuItem>
          </Menu>
        )}
      </div>
      {canVote && !isHost ? (
        <fieldset className="flex flex-col gap-2">
          <legend className="sr-only">{p.multiple ? "Pick one or more" : "Pick one"}</legend>
          {p.options.map((o, i) => {
            const on = picked.includes(i);
            return p.multiple ? (
              <Checkbox key={i} checked={on} onCheckedChange={(c) => setPicked((list) => (c ? [...list, i] : list.filter((x) => x !== i)))} label={o} />
            ) : (
              <label key={i} className={cn("flex min-h-control cursor-pointer items-center gap-3 rounded-md border px-3", on ? "border-primary bg-primary-soft" : "border-border hover:bg-sunken")}>
                <input type="radio" name={`poll-${p.id}`} checked={on} onChange={() => setPicked([i])} className="size-4 accent-primary" />
                <span className="type-body text-fg">{o}</span>
              </label>
            );
          })}
          <Button
            className="self-end"
            disabled={picked.length === 0}
            loading={vote.isPending}
            onClick={() => vote.mutate({ pollId: p.id, choices: picked }, { onSuccess: () => setChanging(false), onError: fail("Couldn't send your vote.") })}
          >
            {voted ? "Change vote" : "Vote"}
          </Button>
        </fieldset>
      ) : (
        <ul className="flex flex-col gap-2">
          {p.options.map((o, i) => {
            const n = p.counts?.[i] ?? 0;
            const pct = total ? Math.round((n / total) * 100) : 0;
            const mine = p.my_choices.includes(i);
            return (
              <li key={i} className="flex flex-col gap-1">
                <div className="flex items-baseline gap-2">
                  <span className="min-w-0 flex-1 type-body text-fg">
                    {o}
                    {mine && <Check className="ml-1 inline size-4 text-primary-strong" aria-label="Your vote" />}
                  </span>
                  {showResults && <span className="type-label tabular-nums text-fg-muted">{pct}% · {n}</span>}
                </div>
                {showResults && (
                  <div className="h-2 overflow-hidden rounded-pill bg-sunken" aria-hidden>
                    <div className="h-full rounded-pill bg-primary transition-[width] duration-300" style={{ width: `${pct}%` }} />
                  </div>
                )}
                {isHost && !p.anonymous && p.votes && (
                  <span className="type-caption text-fg-muted">{p.votes.filter((v) => v.choices.includes(i)).map((v) => v.name).join(", ")}</span>
                )}
              </li>
            );
          })}
        </ul>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <span className="flex-1 type-caption text-fg-muted">
          {showResults || isHost ? `${p.voters} ${p.voters === 1 ? "vote" : "votes"}` : p.status === "closed" ? "Results aren't shared" : voted ? "Vote sent" : ""}
          {p.multiple ? " · more than one choice" : ""}
          {p.anonymous ? " · anonymous" : ""}
        </span>
        {!isHost && voted && p.status === "open" && !changing && (
          <Button size="sm" variant="ghost" onClick={() => (setPicked(p.my_choices), setChanging(true))}>
            Change vote
          </Button>
        )}
        {isHost && p.status === "draft" && (
          <Button size="sm" loading={update.isPending} onClick={() => update.mutate({ pollId: p.id, status: "open" }, { onError: fail("Couldn't launch the poll.") })}>
            Launch
          </Button>
        )}
        {isHost && p.status === "open" && (
          <Button size="sm" variant="secondary" loading={update.isPending} onClick={() => update.mutate({ pollId: p.id, status: "closed" }, { onError: fail("Couldn't end the poll.") })}>
            End poll
          </Button>
        )}
      </div>
    </li>
  );
}

// ───────────────────────────────────────────────────────────── questions ──

type QFilter = "open" | "answered" | "pending";

function QuestionsTab({ data, isHost }: { data: CallsRoomState; isHost: boolean }) {
  const target = useRoomTarget();
  const ask = useAskCallsQuestion(target);
  const [body, setBody] = useState("");
  const [anonymous, setAnonymous] = useState(false);
  const pending = data.questions.filter((q) => q.status === "pending");
  const [filter, setFilter] = useState<QFilter>("open");
  const shown = data.questions.filter((q) =>
    filter === "pending" ? q.status === "pending" : filter === "answered" ? q.status === "answered" : q.status === "open" || (q.status === "pending" && !isHost) || (q.status === "hidden" && isHost),
  );
  const submit = (e?: FormEvent) => {
    e?.preventDefault();
    const text = body.trim();
    if (!text) return;
    ask.mutate(
      { body: text, anonymous },
      {
        onSuccess: (q) => (setBody(""), toast.success(q.status === "pending" ? "Sent to the hosts for approval" : "Question asked")),
        onError: (err) => toast.error(errorMessage(err, "Couldn't ask. Try again.")),
      },
    );
  };
  const options: { value: QFilter; label: string }[] = [
    { value: "open", label: "Open" },
    { value: "answered", label: "Answered" },
    ...(isHost && pending.length > 0 ? [{ value: "pending" as const, label: `Waiting (${pending.length})` }] : []),
  ];
  return (
    <div className="flex flex-col gap-4">
      <form onSubmit={submit} className="flex flex-col gap-3">
        <TextArea value={body} onChange={(e) => setBody(e.target.value)} placeholder="Ask a question" aria-label="Your question" maxLength={500} minRows={2} />
        <div className="flex items-center gap-3">
          <Switch checked={anonymous} onCheckedChange={setAnonymous} label="Ask anonymously" className="flex-1" />
          <Button type="submit" disabled={!body.trim()} loading={ask.isPending}>
            Ask
          </Button>
        </div>
      </form>
      <SegmentedControl options={options} value={filter} onValueChange={setFilter} aria-label="Questions" />
      {shown.length === 0 ? (
        <EmptyState compact icon={<CircleHelp />} title={filter === "answered" ? "Nothing answered yet" : "No questions yet"} />
      ) : (
        <ul className="flex flex-col gap-2">
          {shown.map((q) => (
            <QuestionRow key={q.id} q={q} isHost={isHost} />
          ))}
        </ul>
      )}
    </div>
  );
}

function QuestionRow({ q, isHost }: { q: CallsQuestion; isHost: boolean }) {
  const s = useCallSession();
  const target = useRoomTarget();
  const vote = useVoteCallsQuestion(target);
  const moderate = useModerateCallsQuestion(s.meetingId);
  const del = useDeleteCallsQuestion(target);
  const fail = (msg: string) => (e: unknown) => toast.error(errorMessage(e, msg));
  const set = (status: "open" | "answered" | "hidden") => moderate.mutate({ questionId: q.id, status }, { onError: fail("Couldn't change the question.") });
  const time = new Date(q.created_at).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
  return (
    <li className={cn("flex items-start gap-3 rounded-lg border border-border p-3", q.status === "hidden" && "opacity-60")}>
      <button
        type="button"
        onClick={() => vote.mutate({ questionId: q.id, up: !q.voted }, { onError: fail("Couldn't vote.") })}
        aria-pressed={q.voted}
        aria-label={`${q.voted ? "Remove your vote" : "Vote for this question"} (${q.votes})`}
        disabled={q.status !== "open"}
        className={cn(
          "flex min-w-12 flex-col items-center gap-0.5 rounded-md px-2 py-1 type-label tabular-nums focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-50",
          q.voted ? "bg-primary-soft text-primary-strong" : "text-fg-muted hover:bg-sunken",
        )}
      >
        <ThumbsUp className="size-4" aria-hidden />
        {q.votes}
      </button>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="whitespace-pre-wrap break-words type-body text-fg">{q.body}</p>
        <span className="flex flex-wrap items-center gap-2 type-caption text-fg-muted">
          {q.mine ? "You" : q.name} · {time}
          {q.status === "pending" && <Badge tone="warning">Waiting for a host</Badge>}
          {q.status === "answered" && <Badge tone="success">Answered</Badge>}
          {q.status === "hidden" && <Badge tone="neutral">Hidden</Badge>}
          {q.anonymous && q.mine && <span>(anonymous)</span>}
        </span>
      </div>
      {(isHost || q.mine) && (
        <Menu trigger={<IconButton label="Question options" icon={<MoreVertical />} variant="ghost" size="sm" />}>
          {isHost && q.status === "pending" && (
            <MenuItem icon={<Check />} onSelect={() => set("open")}>
              Approve
            </MenuItem>
          )}
          {isHost && q.status === "open" && (
            <MenuItem icon={<Check />} onSelect={() => set("answered")}>
              Mark answered
            </MenuItem>
          )}
          {isHost && (q.status === "answered" || q.status === "hidden") && (
            <MenuItem icon={<RotateCcw />} onSelect={() => set("open")}>
              Reopen
            </MenuItem>
          )}
          {isHost && q.status !== "hidden" && (
            <MenuItem icon={<EyeOff />} onSelect={() => set("hidden")}>
              Hide
            </MenuItem>
          )}
          <MenuItem icon={<Trash2 />} destructive onSelect={() => del.mutate(q.id, { onError: fail("Couldn't delete the question.") })}>
            Delete
          </MenuItem>
        </Menu>
      )}
    </li>
  );
}
