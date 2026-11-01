"use client";

import {
  callsDeniedMessage,
  callsEndpoints,
  errorMessage,
  isApiError,
  useAuthSession,
  useCallsGuestKnock,
  useCallsGuestMeeting,
  useCallsGuestStatus,
  useCallsGuestToken,
  useCallsJoinInfo,
  useCallsKnockStatus,
  useCancelCallsGuestKnock,
  useCancelCallsKnock,
  useEndpoints,
  useJoinCallsMeeting,
  useKnockCallsMeeting,
  useMe,
  type CallsJoinInfo,
  type CallsJoinToken,
} from "@scaffoldapp/shared";
import { Banner, Button, EmptyState, ErrorState, Skeleton, SkeletonGroup, Spinner } from "@scaffoldapp/ui-web";
import { Ban, CalendarX, Hourglass, Lock, LogIn, PhoneOff, UserX, VideoOff } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { AppMark } from "@/components/shell/AppMark";
import { signInHref } from "@/lib/redirect";
import { PreJoin } from "./PreJoin";
import { CallRoom } from "./room/CallRoom";
import type { CallEnd, CallSession } from "./room/session";
import { readDevicePrefs, readGuestName, readGuestPass, writeGuestName, writeGuestPass, type CallsDevicePrefs, type GuestPass } from "./storage";

type Step = { kind: "prejoin" } | { kind: "knocking"; lobbyId: string } | { kind: "call"; token: CallsJoinToken; prefs: CallsDevicePrefs } | { kind: "over"; why: CallEnd | "denied" };

/**
 * `/meet/<code>`: the public meeting page. Signed-in people get the pre-join screen and
 * join directly when allowed, or ask to join; guests type a name and knock (open meetings
 * only). Then the call, and a closing screen that offers to rejoin.
 */
export function MeetScreen({ code }: { code: string }) {
  const auth = useAuthSession();
  if (!auth.isLoaded) return <MeetFrame><PreJoinSkeleton /></MeetFrame>;
  return auth.isSignedIn ? <MemberMeet code={code} /> : <GuestMeet code={code} />;
}

function MeetFrame({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-dvh flex-col bg-canvas pt-safe pb-safe">
      <header className="mx-auto flex w-full max-w-content items-center px-gutter pt-4">
        <AppMark />
      </header>
      <main className="flex flex-1 flex-col justify-center">{children}</main>
    </div>
  );
}

function PreJoinSkeleton() {
  return (
    <SkeletonGroup label="Loading meeting" className="mx-auto grid w-full max-w-content items-center gap-section px-gutter py-section lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
      <Skeleton className="aspect-video w-full rounded-xl" />
      <div className="flex flex-col gap-6">
        <Skeleton className="h-9 w-64" />
        <Skeleton className="h-12 w-48" />
      </div>
    </SkeletonGroup>
  );
}

function Centered({ children }: { children: ReactNode }) {
  return <div className="mx-auto flex w-full max-w-form flex-col items-center gap-6 px-gutter py-section text-center">{children}</div>;
}

const OVER: Record<CallEnd | "denied", { title: string; icon: ReactNode; rejoin: boolean }> = {
  left: { title: "You left the call", icon: <PhoneOff />, rejoin: true },
  ended: { title: "The call ended", icon: <PhoneOff />, rejoin: false },
  removed: { title: "A host removed you from the call", icon: <UserX />, rejoin: false },
  elsewhere: { title: "You joined from another tab or device", icon: <VideoOff />, rejoin: true },
  failed: { title: "The connection dropped", icon: <VideoOff />, rejoin: true },
  denied: { title: "A host didn't let you in", icon: <Ban />, rejoin: true },
};

function Over({ why, onRejoin, back }: { why: CallEnd | "denied"; onRejoin: () => void; back: ReactNode }) {
  const o = OVER[why];
  return (
    <Centered>
      <EmptyState
        icon={o.icon}
        title={o.title}
        action={
          <div className="flex flex-wrap justify-center gap-3">
            {back}
            {o.rejoin && <Button onClick={onRejoin}>{why === "denied" ? "Ask again" : "Rejoin"}</Button>}
          </div>
        }
      />
    </Centered>
  );
}

function Waiting({ title, onCancel, canceling }: { title: string; onCancel: () => void; canceling: boolean }) {
  return (
    <Centered>
      <span className="flex size-16 items-center justify-center rounded-pill bg-primary-soft text-primary-strong">
        <Hourglass className="size-7 motion-safe:animate-pulse" aria-hidden />
      </span>
      <h1 className="type-title text-fg" role="status">
        Waiting for a host to let you in to {title}
      </h1>
      <Button variant="ghost" onClick={onCancel} loading={canceling}>
        Cancel
      </Button>
    </Centered>
  );
}

// ───────────────────────────────────────────────────────────── people with accounts ──

function sessionTitle(info: CallsJoinInfo, meId: string | undefined): string {
  if (info.title) return info.title;
  if (info.kind === "call") {
    const names = [info.meeting?.host, ...(info.meeting?.invitees.map((i) => i.user) ?? [])].filter((u) => u && u.id !== meId).map((u) => u!.display_name);
    if (names.length === 1) return `Call with ${names[0]}`;
    if (names.length > 1) return `Call with ${names[0]} and ${names.length - 1} ${names.length === 2 ? "other" : "others"}`;
    if (info.host_name) return `Call with ${info.host_name}`;
    return "Call";
  }
  return "Meeting";
}

function MemberMeet({ code }: { code: string }) {
  const info = useCallsJoinInfo(code);
  const me = useMe();
  const join = useJoinCallsMeeting();
  const knock = useKnockCallsMeeting();
  const cancelKnock = useCancelCallsKnock();
  const search = useSearchParams();
  const [step, setStep] = useState<Step>({ kind: "prejoin" });
  const [problem, setProblem] = useState<ReactNode>(null);
  const prefs = useRef<CallsDevicePrefs>(readDevicePrefs());
  const autoTried = useRef(false);
  const data = info.data;

  const doJoin = (p: CallsDevicePrefs) => {
    if (!data) return;
    prefs.current = p;
    setProblem(null);
    join.mutate(data.meeting_id, {
      onSuccess: (token) => setStep({ kind: "call", token, prefs: p }),
      onError: (e) => {
        if (isApiError(e) && e.code === "consent_required") {
          setProblem(
            <Banner tone="warning" action={<Button asChild size="sm" variant="soft"><Link href="/">Review</Link></Button>}>
              Accept the updated terms to join calls.
            </Banner>,
          );
        } else {
          setProblem(<Banner tone="danger">{errorMessage(e, "Couldn't join. Try again.")}</Banner>);
          void info.refetch();
        }
        setStep({ kind: "prejoin" });
      },
    });
  };
  const doKnock = (p: CallsDevicePrefs) => {
    if (!data) return;
    prefs.current = p;
    setProblem(null);
    knock.mutate(data.meeting_id, {
      onSuccess: (entry) => setStep({ kind: "knocking", lobbyId: entry.id }),
      onError: (e) => (isApiError(e) && e.code === "calls_no_knock_needed" ? doJoin(p) : setProblem(<Banner tone="danger">{errorMessage(e, "Couldn't ask to join. Try again.")}</Banner>)),
    });
  };

  // Answering a ring or "Start call" skips the pre-join screen; a knock still
  // waiting from before a refresh resumes.
  useEffect(() => {
    if (!data || step.kind !== "prejoin" || autoTried.current) return;
    autoTried.current = true;
    if (data.lobby?.status === "waiting") setStep({ kind: "knocking", lobbyId: data.lobby.id });
    else if (search.get("autojoin") === "1" && (data.mode === "direct" || data.mode === "host" || data.mode === "cohost")) doJoin(readDevicePrefs());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data]);

  if (info.isPending) return <MeetFrame><PreJoinSkeleton /></MeetFrame>;
  if (info.isError) {
    return (
      <MeetFrame>
        <Centered>
          {info.error.status === 404 ? (
            <EmptyState icon={<CalendarX />} title="Meeting not found" info="Check the code or link." action={<Button asChild variant="secondary"><Link href="/calls">Go to calls</Link></Button>} />
          ) : (
            <ErrorState title="Couldn't load the meeting" info={info.error.message} onRetry={() => void info.refetch()} retrying={info.isFetching} />
          )}
        </Centered>
      </MeetFrame>
    );
  }

  const d = info.data;
  const title = sessionTitle(d, me.data?.id);
  const back = (
    <Button asChild variant="secondary">
      <Link href="/calls">Back to calls</Link>
    </Button>
  );

  if (step.kind === "call") {
    const session: CallSession = {
      token: step.token,
      meetingId: d.meeting_id,
      code: d.code,
      title,
      kind: d.kind,
      role: step.token.role,
      joinUrl: d.meeting?.join_url ?? `${window.location.origin}/meet/${d.code}`,
      guest: null,
    };
    return <CallRoom session={session} prefs={step.prefs} onEnd={(why) => (setStep({ kind: "over", why }), void info.refetch())} />;
  }
  if (step.kind === "over") return <MeetFrame><Over why={step.why} back={back} onRejoin={() => (step.why === "denied" ? doKnock(prefs.current) : doJoin(prefs.current))} /></MeetFrame>;
  if (step.kind === "knocking") {
    return (
      <MeetFrame>
        <KnockWatcher
          lobbyId={step.lobbyId}
          onAdmitted={() => doJoin(prefs.current)}
          onDenied={() => setStep({ kind: "over", why: "denied" })}
          onExpired={() => (setStep({ kind: "prejoin" }), setProblem(<Banner tone="warning">Nobody answered your request. Ask again.</Banner>))}
        />
        <Waiting title={title} canceling={cancelKnock.isPending} onCancel={() => cancelKnock.mutate(step.lobbyId, { onSettled: () => setStep({ kind: "prejoin" }) })} />
      </MeetFrame>
    );
  }

  if (d.mode === "denied") {
    const Icon = d.reason === "locked" ? Lock : d.reason === "canceled" ? CalendarX : Ban;
    return (
      <MeetFrame>
        <Centered>
          <EmptyState icon={<Icon />} title={callsDeniedMessage(d.reason)} action={back} />
        </Centered>
      </MeetFrame>
    );
  }
  return (
    <MeetFrame>
      <PreJoin
        title={title}
        action={d.mode === "knock" ? "knock" : "join"}
        live={d.live}
        displayName={me.data?.display_name ?? ""}
        avatarUrl={me.data?.avatar_url ?? null}
        busy={join.isPending || knock.isPending}
        problem={problem}
        onJoin={(p) => (d.mode === "knock" ? doKnock(p) : doJoin(p))}
      />
    </MeetFrame>
  );
}

/** Follows a signed-in person's knock until a host decides. */
function KnockWatcher({ lobbyId, onAdmitted, onDenied, onExpired }: { lobbyId: string; onAdmitted: () => void; onDenied: () => void; onExpired: () => void }) {
  const status = useCallsKnockStatus(lobbyId);
  const s = status.data?.status;
  const fired = useRef(false);
  useEffect(() => {
    if (!s || s === "waiting" || fired.current) return;
    fired.current = true;
    if (s === "admitted") onAdmitted();
    else if (s === "denied" || s === "removed") onDenied();
    else onExpired();
  }, [s, onAdmitted, onDenied, onExpired]);
  return null;
}

// ───────────────────────────────────────────────────────────── guests ──

/**
 * While a guest waits, closing the page (or leaving it in the app) tells hosts at once with a
 * beacon to `/leave`, so they don't see, or admit, someone who left. Plain text keeps the
 * beacon free of a CORS preflight. A reload checks in again and the knock comes back.
 */
function useLeaveBeacon(url: string | null, key: string | null) {
  const latest = useRef<{ url: string; key: string } | null>(null);
  latest.current = url && key ? { url, key } : null;
  const send = useCallback(() => {
    const l = latest.current;
    if (!l || typeof navigator.sendBeacon !== "function") return;
    try {
      navigator.sendBeacon(l.url, new Blob([JSON.stringify({ key: l.key })], { type: "text/plain" }));
    } catch {
      // Best effort: hosts stop seeing the guest within a minute anyway.
    }
  }, []);
  useEffect(() => {
    window.addEventListener("pagehide", send);
    return () => {
      window.removeEventListener("pagehide", send);
      send(); // unmounted while still waiting (in-app navigation)
    };
  }, [send]);
}

function GuestMeet({ code }: { code: string }) {
  const pm = useCallsGuestMeeting(code);
  const knock = useCallsGuestKnock();
  const token = useCallsGuestToken();
  const cancel = useCancelCallsGuestKnock();
  const [pass, setPassState] = useState<GuestPass | null>(() => readGuestPass(code));
  const [name, setName] = useState(() => pass?.name ?? readGuestName());
  const [step, setStep] = useState<Step>({ kind: "prejoin" });
  const [problem, setProblem] = useState<ReactNode>(null);
  const prefs = useRef<CallsDevicePrefs>(readDevicePrefs());
  const status = useCallsGuestStatus(pass?.lobbyId, pass?.key);
  const setPass = (p: GuestPass | null) => (writeGuestPass(code, p), setPassState(p));
  const api = useEndpoints(callsEndpoints);
  const leave = step.kind === "knocking" && pass ? { url: api.guest.leaveUrl(pass.lobbyId), key: pass.key } : null;
  useLeaveBeacon(leave?.url ?? null, leave?.key ?? null);

  const getToken = (p: GuestPass) =>
    token.mutate(
      { lobbyId: p.lobbyId, key: p.key },
      {
        onSuccess: (t) => setStep({ kind: "call", token: t, prefs: prefs.current }),
        onError: (e) => {
          setPass(null);
          setStep({ kind: "prejoin" });
          setProblem(<Banner tone="danger">{errorMessage(e, "Couldn't join. Ask to join again.")}</Banner>);
        },
      },
    );

  const onJoin = (p: CallsDevicePrefs) => {
    prefs.current = p;
    setProblem(null);
    if (pass && status.data?.status === "admitted") return getToken(pass);
    const clean = name.trim();
    writeGuestName(clean);
    knock.mutate(
      { code, name: clean },
      {
        onSuccess: (entry) => {
          const next = { lobbyId: entry.id, key: entry.key, name: clean };
          setPass(next);
          if (entry.status === "admitted") getToken(next);
          else setStep({ kind: "knocking", lobbyId: entry.id });
        },
        onError: (e) => setProblem(<Banner tone="danger">{errorMessage(e, "Couldn't ask to join. Try again.")}</Banner>),
      },
    );
  };

  // A knock still waiting from before a refresh resumes; decisions arrive by polling.
  const s = status.data?.status;
  useEffect(() => {
    if (!pass || !s) return;
    if (s === "waiting" && step.kind === "prejoin") setStep({ kind: "knocking", lobbyId: pass.lobbyId });
    if (step.kind !== "knocking") return;
    if (s === "admitted") getToken(pass);
    else if (s === "denied" || s === "removed") (setPass(null), setStep({ kind: "over", why: "denied" }));
    else if (s === "expired" || s === "canceled") (setPass(null), setStep({ kind: "prejoin" }), setProblem(<Banner tone="warning">Nobody answered your request. Ask again.</Banner>));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [s, step.kind]);

  if (pm.isPending) return <MeetFrame><PreJoinSkeleton /></MeetFrame>;
  if (pm.isError) {
    return (
      <MeetFrame>
        <Centered>
          {pm.error.status === 404 ? (
            <EmptyState icon={<CalendarX />} title="Meeting not found" info="Check the code or link." />
          ) : (
            <ErrorState title="Couldn't load the meeting" info={pm.error.message} onRetry={() => void pm.refetch()} retrying={pm.isFetching} />
          )}
        </Centered>
      </MeetFrame>
    );
  }
  const m = pm.data;
  const title = m.title || "Meeting";
  const signIn = (
    <Button asChild variant="secondary" leftIcon={<LogIn />}>
      <Link href={signInHref(`/meet/${code}`)}>Sign in</Link>
    </Button>
  );

  if (step.kind === "call" && pass) {
    const session: CallSession = { token: step.token, meetingId: step.token.meeting_id, code, title, kind: "meeting", role: "guest", joinUrl: window.location.href.split("?")[0] ?? "", guest: pass };
    return <CallRoom session={session} prefs={step.prefs} onEnd={(why) => (setStep({ kind: "over", why }), void status.refetch())} />;
  }
  if (step.kind === "over") {
    return (
      <MeetFrame>
        <Over why={step.why} back={signIn} onRejoin={() => (pass && status.data?.status === "admitted" ? getToken(pass) : setStep({ kind: "prejoin" }))} />
      </MeetFrame>
    );
  }
  if (step.kind === "knocking" && pass) {
    return (
      <MeetFrame>
        <Waiting
          title={title}
          canceling={cancel.isPending}
          onCancel={() => cancel.mutate({ lobbyId: pass.lobbyId, key: pass.key }, { onSettled: () => (setPass(null), setStep({ kind: "prejoin" })) })}
        />
      </MeetFrame>
    );
  }
  if (!m.guests_allowed) {
    return (
      <MeetFrame>
        <Centered>
          {m.canceled ? (
            <EmptyState icon={<CalendarX />} title="This meeting was canceled" />
          ) : m.locked ? (
            <EmptyState icon={<Lock />} title="The host locked this meeting" />
          ) : (
            <EmptyState icon={<LogIn />} title="Sign in to join this meeting" action={signIn} />
          )}
        </Centered>
      </MeetFrame>
    );
  }
  const rejoining = !!pass && status.data?.status === "admitted";
  return (
    <MeetFrame>
      <PreJoin
        title={title}
        action={rejoining || !m.lobby_enabled ? "join" : "knock"}
        live={m.live}
        {...(rejoining ? {} : { name: { value: name, onChange: setName } })}
        displayName={name}
        busy={knock.isPending || token.isPending}
        problem={problem}
        onJoin={onJoin}
        footer={
          <p className="type-label text-fg-muted">
            Have an account?{" "}
            <Link href={signInHref(`/meet/${code}`)} className="font-semibold text-primary-strong underline-offset-2 hover:underline">
              Sign in
            </Link>
          </p>
        }
      />
      {token.isPending && <Spinner className="sr-only" label="Joining" />}
    </MeetFrame>
  );
}
