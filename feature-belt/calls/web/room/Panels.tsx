"use client";

import { useParticipants, type ReceivedChatMessage } from "@livekit/components-react";
import {
  CALLS_ROLE_LABELS,
  callsIdentityUserId,
  errorMessage,
  isCallsHostRole,
  useAdmitCallsLobby,
  useCallsLobby,
  useCallsMeetingRings,
  useDenyCallsLobby,
  useMuteCallsParticipant,
  useRemoveCallsParticipant,
  useRingCallsAgain,
  useSetCallsHand,
  useSetCallsInviteeRole,
  type CallsRing,
  type CallsUser,
} from "@scaffoldapp/shared";
import { Avatar, Button, ConfirmDialog, Dialog, EmptyState, IconButton, InfoTip, Menu, MenuItem, Spinner, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { Track, type Participant } from "livekit-client";
import { Hand, MessageSquare, Mic, MicOff, MoreVertical, Phone, SendHorizontal, ShieldCheck, ShieldOff, UserMinus, UserPlus, VolumeX } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { CallsPeoplePicker } from "../PeoplePicker";
import { useCallSession } from "./session";
import { useParticipantFacts } from "./Tile";

// ───────────────────────────────────────────────────────────── people ──

/** Everyone in the call, people waiting to join (hosts), and who an instant call is ringing. */
export function PeoplePanel({ isHost }: { isHost: boolean }) {
  const s = useCallSession();
  const participants = useParticipants();
  const mute = useMuteCallsParticipant(s.meetingId);
  const [adding, setAdding] = useState(false);
  const signedInHost = isHost && !s.guest;

  return (
    <div className="flex flex-col gap-6">
      {signedInHost && <LobbySection />}
      <RingSection canRing={!s.guest} />
      <section className="flex flex-col gap-3" aria-label="In the call">
        <div className="flex min-h-control items-center gap-2">
          <h3 className="flex-1 type-title-sm text-fg">In the call ({participants.length})</h3>
          {signedInHost && (
            <Button
              size="sm"
              variant="ghost"
              leftIcon={<VolumeX />}
              loading={mute.isPending}
              onClick={() =>
                mute.mutate(
                  { all: true },
                  {
                    onSuccess: (r) => toast.success(r.muted === 0 ? "Everyone is already muted" : `Muted ${r.muted} ${r.muted === 1 ? "person" : "people"}`),
                    onError: (e) => toast.error(errorMessage(e, "Couldn't mute everyone.")),
                  },
                )
              }
            >
              Mute all
            </Button>
          )}
        </div>
        <ParticipantList participants={participants} isHost={signedInHost} />
        {signedInHost && (
          <Button variant="secondary" leftIcon={<UserPlus />} onClick={() => setAdding(true)}>
            Add people
          </Button>
        )}
      </section>
      {signedInHost && <AddPeopleDialog open={adding} onOpenChange={setAdding} inCall={participants.map((p) => callsIdentityUserId(p.identity)).filter((x): x is string => !!x)} />}
    </div>
  );
}

function ParticipantList({ participants, isHost }: { participants: Participant[]; isHost: boolean }) {
  // You first, then raised hands (earliest first), then by name.
  const hand = (p: Participant) => Number(p.attributes["hand"] || 0);
  const sorted = [...participants].sort((a, b) => {
    if (a.isLocal !== b.isLocal) return a.isLocal ? -1 : 1;
    if (!!hand(a) !== !!hand(b)) return hand(a) ? -1 : 1;
    if (hand(a) && hand(b)) return hand(a) - hand(b);
    return (a.name ?? "").localeCompare(b.name ?? "");
  });
  return (
    <ul className="flex flex-col">
      {sorted.map((p) => (
        <ParticipantRow key={p.identity} participant={p} isHost={isHost} />
      ))}
    </ul>
  );
}

function ParticipantRow({ participant: p, isHost }: { participant: Participant; isHost: boolean }) {
  const s = useCallSession();
  const facts = useParticipantFacts(p);
  const micOn = p.isMicrophoneEnabled;
  const hasMic = !!p.getTrackPublication(Track.Source.Microphone);
  const mute = useMuteCallsParticipant(s.meetingId);
  const remove = useRemoveCallsParticipant(s.meetingId);
  const role = useSetCallsInviteeRole(s.meetingId);
  const hand = useSetCallsHand(s.meetingId);
  const [removing, setRemoving] = useState(false);
  const userId = callsIdentityUserId(p.identity);
  const targetIsHost = facts.meta.role === "host";
  const canManage = isHost && !p.isLocal && !targetIsHost;
  const fail = (msg: string) => (e: unknown) => toast.error(errorMessage(e, msg));

  return (
    <li className="flex min-h-row items-center gap-3 py-2">
      <Avatar name={facts.name} src={facts.meta.avatar_url ?? null} size="sm" />
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate type-body text-fg">
          {facts.name}
          {p.isLocal ? " (you)" : ""}
        </span>
        {(isCallsHostRole(facts.meta.role) || facts.meta.role === "guest") && <span className="type-caption text-fg-muted">{CALLS_ROLE_LABELS[facts.meta.role]}</span>}
      </div>
      {facts.hand && <Hand className="size-5 shrink-0 text-warning-strong" aria-label="Hand raised" />}
      {hasMic ? micOn ? <Mic className="size-5 shrink-0 text-fg-muted" aria-label="Microphone on" /> : <MicOff className="size-5 shrink-0 text-danger-strong" aria-label="Muted" /> : null}
      {canManage && (
        <Menu trigger={<IconButton label={`Manage ${facts.name}`} icon={<MoreVertical />} variant="ghost" size="sm" />}>
          {micOn && (
            <MenuItem icon={<MicOff />} onSelect={() => mute.mutate({ identity: p.identity }, { onSuccess: () => toast.success(`Muted ${facts.name}`), onError: fail("Couldn't mute.") })}>
              Mute
            </MenuItem>
          )}
          {facts.hand && (
            <MenuItem icon={<Hand />} onSelect={() => hand.mutate({ raised: false, identity: p.identity }, { onError: fail("Couldn't lower their hand.") })}>
              Lower hand
            </MenuItem>
          )}
          {userId && s.role === "host" && (
            <MenuItem
              icon={facts.meta.role === "cohost" ? <ShieldOff /> : <ShieldCheck />}
              onSelect={() =>
                role.mutate(
                  { userId, role: facts.meta.role === "cohost" ? "invitee" : "cohost" },
                  { onSuccess: () => toast.success(facts.meta.role === "cohost" ? `${facts.name} is no longer a co-host` : `${facts.name} is now a co-host`), onError: fail("Couldn't change their role.") },
                )
              }
            >
              {facts.meta.role === "cohost" ? "Remove as co-host" : "Make co-host"}
            </MenuItem>
          )}
          {(facts.meta.role !== "cohost" || s.role === "host") && (
            <MenuItem icon={<UserMinus />} destructive onSelect={() => setRemoving(true)}>
              Remove from call
            </MenuItem>
          )}
        </Menu>
      )}
      <ConfirmDialog
        open={removing}
        onOpenChange={setRemoving}
        title={`Remove ${facts.name}?`}
        info="They leave the call and can't rejoin it."
        confirmLabel="Remove"
        tone="danger"
        onConfirm={() => remove.mutateAsync(p.identity).then(() => toast.success(`Removed ${facts.name}`))}
        errorText={(e) => errorMessage(e, "Couldn't remove them. Try again.")}
      />
    </li>
  );
}

function LobbySection() {
  const s = useCallSession();
  const lobby = useCallsLobby(s.meetingId);
  const admit = useAdmitCallsLobby(s.meetingId);
  const deny = useDenyCallsLobby(s.meetingId);
  const waiting = lobby.data?.items ?? [];
  if (waiting.length === 0) return null;
  const fail = (e: unknown) => toast.error(errorMessage(e, "Couldn't update the request. Try again."));
  return (
    <section className="flex flex-col gap-3" aria-label="Waiting to join">
      <div className="flex min-h-control items-center gap-2">
        <h3 className="flex flex-1 items-center gap-2 type-title-sm text-fg">
          Waiting to join ({waiting.length})
          <InfoTip content="Let people in or deny them. People you deny can ask again." />
        </h3>
        {waiting.length > 1 && (
          <Button size="sm" variant="soft" onClick={() => admit.mutate({ all: true }, { onError: fail })}>
            Admit all
          </Button>
        )}
      </div>
      <ul className="flex flex-col gap-1 rounded-md bg-warning-soft p-2">
        {waiting.map((e) => (
          <li key={e.id} className="flex min-h-row items-center gap-3 px-1">
            <Avatar name={e.name} src={e.user?.avatar_url ?? null} size="sm" />
            <span className="flex min-w-0 flex-1 flex-col">
              <span className="truncate type-body text-fg">{e.name}</span>
              <span className="type-caption text-fg-muted">{e.guest ? "Guest" : "Has an account"}</span>
            </span>
            <Button size="sm" variant="ghost" onClick={() => deny.mutate({ ids: [e.id] }, { onError: fail })}>
              Deny
            </Button>
            <Button size="sm" onClick={() => admit.mutate({ ids: [e.id] }, { onError: fail })}>
              Admit
            </Button>
          </li>
        ))}
      </ul>
    </section>
  );
}

const RING_LABEL: Record<CallsRing["status"], string> = { ringing: "Ringing…", accepted: "Answered", declined: "Declined", missed: "No answer" };

function RingSection({ canRing }: { canRing: boolean }) {
  const s = useCallSession();
  const rings = useCallsMeetingRings(s.meetingId, !s.guest).filter((r) => r.status !== "accepted");
  const again = useRingCallsAgain(s.meetingId);
  if (rings.length === 0) return null;
  return (
    <section className="flex flex-col gap-3" aria-label="Calling">
      <h3 className="type-title-sm text-fg">Calling</h3>
      <ul className="flex flex-col">
        {rings.map((r) => (
          <li key={r.id} className="flex min-h-row items-center gap-3 py-2">
            <Avatar name={r.callee.display_name} src={r.callee.avatar_url} size="sm" />
            <span className="flex min-w-0 flex-1 flex-col">
              <span className="truncate type-body text-fg">{r.callee.display_name}</span>
              <span className={cn("type-caption", r.status === "ringing" ? "text-primary-strong" : "text-fg-muted")}>{RING_LABEL[r.status]}</span>
            </span>
            {canRing && r.status !== "ringing" && (
              <Button
                size="sm"
                variant="soft"
                leftIcon={<Phone />}
                loading={again.isPending && again.variables?.[0] === r.callee.id}
                onClick={() => again.mutate([r.callee.id], { onError: (e) => toast.error(errorMessage(e, "Couldn't ring them.")) })}
              >
                Ring again
              </Button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}

function AddPeopleDialog({ open, onOpenChange, inCall }: { open: boolean; onOpenChange: (o: boolean) => void; inCall: string[] }) {
  const s = useCallSession();
  const ring = useRingCallsAgain(s.meetingId);
  const [people, setPeople] = useState<CallsUser[]>([]);
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Add people"
      info="Their devices ring. They join this call when they answer."
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            leftIcon={<Phone />}
            disabled={people.length === 0}
            loading={ring.isPending}
            onClick={() =>
              ring.mutate(
                people.map((p) => p.id),
                {
                  onSuccess: () => {
                    toast.success(people.length === 1 ? `Ringing ${people[0]?.display_name}` : `Ringing ${people.length} people`);
                    setPeople([]);
                    onOpenChange(false);
                  },
                  onError: (e) => toast.error(errorMessage(e, "Couldn't ring them.")),
                },
              )
            }
          >
            Ring
          </Button>
        </>
      }
    >
      <CallsPeoplePicker selected={people} onChange={setPeople} exclude={inCall} />
    </Dialog>
  );
}

// ───────────────────────────────────────────────────────────── chat ──

/** In-call chat over the room's data channel: only people in the call see it, and it isn't kept. */
export function ChatPanel({ messages, send, sending }: { messages: ReceivedChatMessage[]; send: (text: string) => Promise<unknown>; sending: boolean }) {
  const [text, setText] = useState("");
  const listRef = useRef<HTMLOListElement>(null);
  useEffect(() => {
    listRef.current?.lastElementChild?.scrollIntoView({ block: "end" });
  }, [messages.length]);

  const submit = (e?: FormEvent) => {
    e?.preventDefault();
    const body = text.trim();
    if (!body || sending) return;
    send(body)
      .then(() => setText(""))
      .catch(() => toast.error("Couldn't send. Check your connection."));
  };

  return (
    <div className="flex h-full min-h-0 flex-col gap-3">
      <p className="flex items-center gap-2 type-caption text-fg-muted">
        Messages last only for this call
        <InfoTip content="Only people in the call see them, from when they joined. Nothing is saved." />
      </p>
      {messages.length === 0 ? (
        <EmptyState compact icon={<MessageSquare />} title="No messages yet" className="flex-1" />
      ) : (
        <ol ref={listRef} className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto" aria-live="polite" aria-label="Chat messages">
          {messages.map((m, i) => {
            const prev = messages[i - 1];
            const grouped = prev && prev.from?.identity === m.from?.identity && m.timestamp - prev.timestamp < 60_000;
            return (
              <li key={m.id} className={cn("flex flex-col gap-0.5", grouped && "-mt-2")}>
                {!grouped && (
                  <span className="flex items-baseline gap-2">
                    <span className="truncate type-label font-semibold text-fg">{m.from?.isLocal ? "You" : m.from?.name || "Guest"}</span>
                    <span className="type-caption text-fg-subtle">{new Date(m.timestamp).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" })}</span>
                  </span>
                )}
                <p className="whitespace-pre-wrap break-words type-body text-fg">{m.message}</p>
              </li>
            );
          })}
        </ol>
      )}
      <form onSubmit={submit} className="flex items-end gap-2">
        <TextArea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault();
              submit();
            }
          }}
          placeholder="Send a message"
          aria-label="Message"
          maxLength={2000}
          rows={1}
          autoGrow
          className="flex-1"
        />
        <IconButton type="submit" label="Send" icon={sending ? <Spinner size={16} /> : <SendHorizontal />} variant="primary" disabled={!text.trim()} />
      </form>
    </div>
  );
}

