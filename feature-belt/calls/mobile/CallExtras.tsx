import { useDataChannel, useRoomInfo } from "@livekit/components-react";
import {
  CALLS_CAPTION_TOPIC,
  errorMessage,
  formatCallsCountdown,
  callsSecondsLeft,
  decodeCallsCaption,
  parseCallsRoomMeta,
  useAskCallsQuestion,
  useCallsRoomState,
  useCreateCallsPoll,
  useModerateCallsQuestion,
  useNow,
  useUpdateCallsPoll,
  useVoteCallsPoll,
  useVoteCallsQuestion,
  type CallsMyBreakout,
  type CallsPoll,
  type CallsQuestion,
  type CallsRoomMeta,
  type CallsRoomTarget,
} from "@scaffoldapp/shared";
import { Badge, Banner, Button, Checkbox, Dialog, EmptyState, ErrorState, IconButton, Input, SegmentedControl, Sheet, Spinner, Switch, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { BarChart3, Check, CircleHelp, Circle as CircleIcon, ThumbsUp } from "lucide-react-native";
import { useEffect, useMemo, useState } from "react";
import { Pressable, ScrollView, View } from "react-native";

/** The room's server-owned metadata (recording, rules, breakout rooms). */
export function useRoomMeta(): CallsRoomMeta {
  const info = useRoomInfo();
  return useMemo(() => parseCallsRoomMeta(info.metadata), [info.metadata]);
}

/** "Recording" / "Transcript on" pills for the call's header. */
export function RecordingPills({ meta }: { meta: CallsRoomMeta }) {
  const theme = useTheme();
  if (!meta.recording && !meta.transcribing) return null;
  return (
    <View style={{ flexDirection: "row", gap: theme.space[2] }} accessibilityRole="text" accessibilityLiveRegion="polite">
      {meta.recording ? (
        <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[1], backgroundColor: theme.colors.danger, borderRadius: theme.radius.pill, paddingHorizontal: theme.space[3], paddingVertical: theme.space[1] }}>
          <CircleIcon size={10} color={theme.colors.dangerFg} fill={theme.colors.dangerFg} />
          <Text role="label" weight={600} style={{ color: theme.colors.dangerFg }}>
            Recording
          </Text>
        </View>
      ) : null}
      {meta.transcribing ? <Badge tone="warning">Transcript</Badge> : null}
    </View>
  );
}

const acknowledged = new Set<string>();

/** Tells you the call is recorded or transcribed (once per recording); staying is agreeing. */
export function RecordingConsent({ meta, onLeave }: { meta: CallsRoomMeta; onLeave: () => void }) {
  const [, setTick] = useState(0);
  const transcriptKey = `transcript:${meta.meeting_id}`;
  useEffect(() => {
    if (!meta.transcribing) acknowledged.delete(transcriptKey);
  }, [meta.transcribing, transcriptKey]);
  const recording = meta.recording && !acknowledged.has(meta.recording.id);
  const transcript = meta.transcribing && !acknowledged.has(transcriptKey);
  if (!recording && !transcript) return null;
  const ack = () => {
    if (meta.recording) acknowledged.add(meta.recording.id);
    if (meta.transcribing) acknowledged.add(transcriptKey);
    setTick((n) => n + 1);
  };
  return (
    <Dialog
      open
      onClose={ack}
      title={recording && transcript ? "This call is being recorded and transcribed" : recording ? "This call is being recorded" : "This call's transcript is being saved"}
      info="Hosts decide who can open recordings and transcripts afterwards."
      footer={
        <>
          <Button fullWidth onPress={ack}>
            Got it
          </Button>
          <Button fullWidth variant="ghost" onPress={onLeave}>
            Leave call
          </Button>
        </>
      }
    >
      <Text>Staying in the call means you agree.</Text>
    </Dialog>
  );
}

/** Other people's captions (each speaker's browser sends them); this app shows them. */
export function useCaptionLines(show: boolean): { name: string; text: string }[] {
  const [lines, setLines] = useState<Record<string, { name: string; text: string; at: number }>>({});
  useDataChannel(CALLS_CAPTION_TOPIC, (msg) => {
    if (!show || !msg.from) return;
    const c = decodeCallsCaption(msg.payload);
    if (c?.text) setLines((prev) => ({ ...prev, [msg.from!.identity]: { name: msg.from!.name || "Someone", text: c.text, at: Date.now() } }));
  });
  const now = useNow(1000, show);
  return Object.values(lines)
    .filter((l) => now - l.at < 6000)
    .sort((a, b) => a.at - b.at)
    .slice(-3);
}

export function CaptionsOverlay({ lines }: { lines: { name: string; text: string }[] }) {
  const theme = useTheme();
  if (lines.length === 0) return null;
  return (
    <View pointerEvents="none" accessibilityLiveRegion="polite" style={{ position: "absolute", left: theme.space[3], right: theme.space[3], bottom: theme.space[3], backgroundColor: theme.colors.overlay, borderRadius: theme.radius.lg, padding: theme.space[3], gap: theme.space[1] }}>
      {lines.map((l, i) => (
        <Text key={i} style={{ color: theme.colors.ink[50] }}>
          <Text weight={600} style={{ color: theme.colors.ink[300] }}>{`${l.name}: `}</Text>
          {l.text}
        </Text>
      ))}
    </View>
  );
}

/** Breakout rooms: your room from the main room, or the room you're in with time left. */
export function BreakoutBanner({ meta, mine, inBreakout, busy, onJoin, onReturn }: { meta: CallsRoomMeta; mine: CallsMyBreakout | null; inBreakout: boolean; busy: boolean; onJoin: (roomId?: string) => void; onReturn: () => void }) {
  const theme = useTheme();
  const now = useNow(1000, !!meta.breakout);
  const b = meta.breakout;
  if (inBreakout && b?.room) {
    const left = callsSecondsLeft(b.status === "closing" ? b.closing_at : b.ends_at, now);
    return (
      <Banner
        tone={b.status === "closing" ? "warning" : "info"}
        title={`${b.room.name}${b.status === "closing" ? ` · back in ${formatCallsCountdown(left ?? 0)}` : left !== null ? ` · ${formatCallsCountdown(left)} left` : ""}`}
        action={{ label: "Main room", onPress: onReturn }}
        style={{ marginHorizontal: theme.space[4] }}
      />
    );
  }
  if (!inBreakout && mine && mine.status === "open" && (mine.room || mine.allow_choose)) {
    const first = mine.rooms[0];
    return (
      <Banner
        tone="info"
        title={mine.room ? `A host moved you to ${mine.room.name}` : "Breakout rooms are open"}
        action={busy ? undefined : { label: mine.room ? "Join" : `Join ${first?.name ?? "a room"}`, onPress: () => onJoin(mine.room ? undefined : first?.id) }}
        style={{ marginHorizontal: theme.space[4] }}
      />
    );
  }
  return null;
}

// ───────────────────────────────────────────────────────────── polls and Q&A ──

/** Polls and questions in a sheet: vote, ask, upvote; hosts launch, end and moderate. */
export function ActivitiesSheet({ open, onClose, target, isHost }: { open: boolean; onClose: () => void; target: CallsRoomTarget; isHost: boolean }) {
  const theme = useTheme();
  const state = useCallsRoomState(target, open);
  const [tab, setTab] = useState<"polls" | "questions">("polls");
  return (
    <Sheet open={open} onClose={onClose} title="Polls and Q&A" snapPoints={["80%"]}>
      <View style={{ gap: theme.space[4], flex: 1 }}>
        <SegmentedControl
          options={[
            { value: "polls", label: "Polls" },
            { value: "questions", label: "Q&A" },
          ]}
          value={tab}
          onValueChange={setTab}
          accessibilityLabel="Polls or questions"
        />
        {state.isPending ? (
          <Spinner label="Loading" />
        ) : state.isError ? (
          <ErrorState title="Couldn't load this" info={state.error.message} onRetry={() => void state.refetch()} retrying={state.isFetching} />
        ) : tab === "polls" ? (
          <Polls polls={[...state.data.polls].reverse()} target={target} isHost={isHost} />
        ) : (
          <Questions questions={state.data.questions} target={target} isHost={isHost} />
        )}
      </View>
    </Sheet>
  );
}

function Polls({ polls, target, isHost }: { polls: CallsPoll[]; target: CallsRoomTarget; isHost: boolean }) {
  const theme = useTheme();
  const [creating, setCreating] = useState(false);
  return (
    <ScrollView contentContainerStyle={{ gap: theme.space[3] }}>
      {isHost && (creating ? <NewPoll meetingId={target.meetingId} onDone={() => setCreating(false)} /> : <Button variant="secondary" onPress={() => setCreating(true)}>New poll</Button>)}
      {polls.length === 0 && !creating ? <EmptyState icon={BarChart3} title={isHost ? "No polls yet" : "No polls right now"} /> : null}
      {polls.map((p) => (
        <PollCard key={p.id} poll={p} target={target} isHost={isHost} />
      ))}
    </ScrollView>
  );
}

function NewPoll({ meetingId, onDone }: { meetingId: string; onDone: () => void }) {
  const theme = useTheme();
  const create = useCreateCallsPoll(meetingId);
  const [question, setQuestion] = useState("");
  const [options, setOptions] = useState(["", ""]);
  const [anonymous, setAnonymous] = useState(true);
  const filled = options.map((o) => o.trim()).filter(Boolean);
  const valid = question.trim() !== "" && filled.length >= 2 && new Set(filled).size === filled.length;
  return (
    <View style={{ gap: theme.space[3], borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.lg, padding: theme.space[4] }}>
      <Input value={question} onChangeText={setQuestion} placeholder="Question" accessibilityLabel="Question" maxLength={300} />
      {options.map((o, i) => (
        <Input key={i} value={o} onChangeText={(v) => setOptions((list) => list.map((x, j) => (j === i ? v : x)))} placeholder={`Option ${i + 1}`} accessibilityLabel={`Option ${i + 1}`} maxLength={100} />
      ))}
      {options.length < 10 && (
        <Button variant="ghost" onPress={() => setOptions((l) => [...l, ""])}>
          Add option
        </Button>
      )}
      <Switch value={anonymous} onValueChange={setAnonymous} label="Anonymous" info="Hosts see totals, not who voted for what." />
      <Button
        disabled={!valid}
        loading={create.isPending}
        onPress={() =>
          create.mutate(
            { question, options: filled, anonymous, open: true },
            { onSuccess: () => (toast.success("Poll launched"), onDone()), onError: (e) => toast.error(errorMessage(e, "Couldn't launch the poll.")) },
          )
        }
      >
        Launch
      </Button>
      <Button variant="ghost" onPress={onDone}>
        Cancel
      </Button>
    </View>
  );
}

function PollCard({ poll: p, target, isHost }: { poll: CallsPoll; target: CallsRoomTarget; isHost: boolean }) {
  const theme = useTheme();
  const vote = useVoteCallsPoll(target);
  const update = useUpdateCallsPoll(target.meetingId);
  const [picked, setPicked] = useState<number[]>(p.my_choices);
  const voted = p.my_choices.length > 0;
  const canVote = !isHost && p.status === "open";
  const total = p.counts?.reduce((a, b) => a + b, 0) ?? 0;
  const fail = (msg: string) => (e: unknown) => toast.error(errorMessage(e, msg));
  return (
    <View style={{ gap: theme.space[3], borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.lg, padding: theme.space[4] }}>
      <View style={{ flexDirection: "row", gap: theme.space[2], alignItems: "flex-start" }}>
        <Text weight={600} style={{ flex: 1 }}>
          {p.question}
        </Text>
        <Badge tone={p.status === "open" ? "success" : "neutral"}>{p.status === "draft" ? "Draft" : p.status === "open" ? "Live" : "Ended"}</Badge>
      </View>
      {p.options.map((o, i) => {
        const n = p.counts?.[i] ?? 0;
        const pct = total ? Math.round((n / total) * 100) : 0;
        const on = picked.includes(i);
        return canVote ? (
          p.multiple ? (
            <Checkbox key={i} checked={on} onCheckedChange={(c) => setPicked((l) => (c ? [...l, i] : l.filter((x) => x !== i)))} label={o} />
          ) : (
            <Pressable
              key={i}
              onPress={() => setPicked([i])}
              accessibilityRole="radio"
              accessibilityState={{ checked: on }}
              style={{ minHeight: theme.space[12], justifyContent: "center", paddingHorizontal: theme.space[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: on ? theme.colors.primary : theme.colors.border, backgroundColor: on ? theme.colors.primarySoft : "transparent" }}
            >
              <Text>{o}</Text>
            </Pressable>
          )
        ) : (
          <View key={i} style={{ gap: theme.space[1] }}>
            <View style={{ flexDirection: "row", gap: theme.space[2] }}>
              <Text style={{ flex: 1 }}>{o}</Text>
              {p.my_choices.includes(i) ? <Check size={16} color={theme.colors.primaryStrong} accessibilityLabel="Your vote" /> : null}
              {p.counts ? <Text role="label" tone="muted">{`${pct}% · ${n}`}</Text> : null}
            </View>
            {p.counts ? (
              <View style={{ height: theme.space[2], borderRadius: theme.radius.pill, backgroundColor: theme.colors.sunken, overflow: "hidden" }}>
                <View style={{ width: `${pct}%`, height: "100%", backgroundColor: theme.colors.primary }} />
              </View>
            ) : null}
          </View>
        );
      })}
      {canVote && (
        <Button disabled={picked.length === 0} loading={vote.isPending} onPress={() => vote.mutate({ pollId: p.id, choices: picked }, { onError: fail("Couldn't send your vote.") })}>
          {voted ? "Change vote" : "Vote"}
        </Button>
      )}
      {isHost && p.status === "draft" && (
        <Button loading={update.isPending} onPress={() => update.mutate({ pollId: p.id, status: "open" }, { onError: fail("Couldn't launch the poll.") })}>
          Launch
        </Button>
      )}
      {isHost && p.status === "open" && (
        <Button variant="secondary" loading={update.isPending} onPress={() => update.mutate({ pollId: p.id, status: "closed" }, { onError: fail("Couldn't end the poll.") })}>
          End poll
        </Button>
      )}
      {isHost && p.status === "closed" && (
        <Button variant="secondary" onPress={() => update.mutate({ pollId: p.id, results_shared: !p.results_shared }, { onError: fail("Couldn't change that.") })}>
          {p.results_shared ? "Hide results" : "Share results"}
        </Button>
      )}
    </View>
  );
}

function Questions({ questions, target, isHost }: { questions: CallsQuestion[]; target: CallsRoomTarget; isHost: boolean }) {
  const theme = useTheme();
  const ask = useAskCallsQuestion(target);
  const vote = useVoteCallsQuestion(target);
  const moderate = useModerateCallsQuestion(target.meetingId);
  const [body, setBody] = useState("");
  const [anonymous, setAnonymous] = useState(false);
  const fail = (msg: string) => (e: unknown) => toast.error(errorMessage(e, msg));
  return (
    <ScrollView contentContainerStyle={{ gap: theme.space[3] }} keyboardShouldPersistTaps="handled">
      <Input value={body} onChangeText={setBody} placeholder="Ask a question" accessibilityLabel="Your question" maxLength={500} multiline />
      <Switch value={anonymous} onValueChange={setAnonymous} label="Ask anonymously" />
      <Button
        disabled={!body.trim()}
        loading={ask.isPending}
        onPress={() =>
          ask.mutate(
            { body: body.trim(), anonymous },
            { onSuccess: (q) => (setBody(""), toast.success(q.status === "pending" ? "Sent to the hosts for approval" : "Question asked")), onError: fail("Couldn't ask. Try again.") },
          )
        }
      >
        Ask
      </Button>
      {questions.length === 0 ? <EmptyState icon={CircleHelp} title="No questions yet" /> : null}
      {questions.map((q) => (
        <View key={q.id} style={{ flexDirection: "row", gap: theme.space[3], borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.lg, padding: theme.space[3], opacity: q.status === "hidden" ? 0.6 : 1 }}>
          <View style={{ alignItems: "center" }}>
            <IconButton label={`${q.voted ? "Remove your vote" : "Vote for this question"} (${q.votes})`} icon={ThumbsUp} variant={q.voted ? "primary" : "ghost"} disabled={q.status !== "open"} onPress={() => vote.mutate({ questionId: q.id, up: !q.voted }, { onError: fail("Couldn't vote.") })} />
            <Text role="label" tone="muted">
              {String(q.votes)}
            </Text>
          </View>
          <View style={{ flex: 1, gap: theme.space[1] }}>
            <Text>{q.body}</Text>
            <Text role="caption" tone="muted">
              {`${q.mine ? "You" : q.name}${q.status === "pending" ? " · waiting for a host" : q.status === "answered" ? " · answered" : q.status === "hidden" ? " · hidden" : ""}`}
            </Text>
            {isHost && q.status === "pending" ? (
              <Button size="md" variant="soft" onPress={() => moderate.mutate({ questionId: q.id, status: "open" }, { onError: fail("Couldn't approve it.") })}>
                Approve
              </Button>
            ) : null}
            {isHost && q.status === "open" ? (
              <Button size="md" variant="soft" onPress={() => moderate.mutate({ questionId: q.id, status: "answered" }, { onError: fail("Couldn't change it.") })}>
                Mark answered
              </Button>
            ) : null}
          </View>
        </View>
      ))}
    </ScrollView>
  );
}
