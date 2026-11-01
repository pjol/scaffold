"use client";

import { errorMessage, useAcceptCallsRing, useCallsRings, useDeclineCallsRing, useNow, type CallsRing } from "@scaffoldapp/shared";
import { Button, IconButton, Presence, toast } from "@scaffoldapp/ui-web";
import { BellOff, BellRing, PhoneOff, Video } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { createRingtone, type Ringtone } from "./ringtone";

/**
 * Session mount: when a call rings for you, an incoming-call card (top of the screen on
 * phones, bottom right on desktop) with Accept, Decline and a ringtone you can mute. It
 * hides while you're already on that meeting's page and when the ring ends anywhere
 * (answered on another device, missed, the caller hung up).
 */
export function CallsRinger() {
  const rings = useCallsRings();
  const pathname = usePathname() ?? "";
  const now = useNow(1000, (rings.data?.items.length ?? 0) > 0);
  const ring = rings.data?.items.find((r) => r.status === "ringing" && Date.parse(r.expires_at) > now && pathname !== `/meet/${r.code}`);
  // The fixed layer is the animated element itself: a transformed ancestor would pin a
  // fixed child to it instead of the viewport.
  return (
    <Presence show={!!ring} className="fixed inset-x-0 top-0 z-(--app-z-modal) px-4 pt-4 sm:inset-x-auto sm:top-auto sm:right-6 sm:bottom-6 sm:px-0 sm:pt-0">
      {ring ? <IncomingCall key={ring.id} ring={ring} /> : null}
    </Presence>
  );
}

function IncomingCall({ ring }: { ring: CallsRing }) {
  const router = useRouter();
  const accept = useAcceptCallsRing();
  const decline = useDeclineCallsRing();
  const [muted, setMuted] = useState(false);
  const [silent, setSilent] = useState(false);
  const tone = useRef<Ringtone | null>(null);

  useEffect(() => {
    if (muted) return undefined;
    const t = createRingtone();
    tone.current = t;
    void t.start().then((ok) => setSilent(!ok));
    return () => {
      t.stop();
      tone.current = null;
    };
  }, [muted]);

  useEffect(() => {
    const previous = document.title;
    document.title = `${ring.caller.display_name} is calling`;
    return () => {
      document.title = previous;
    };
  }, [ring.caller.display_name]);

  const group = ring.others.length > 0;
  const what = ring.title || (group ? `Group call · ${ring.others.length + 2} people` : "Video call");

  const onAccept = () => {
    tone.current?.stop();
    accept.mutate(ring.id, {
      onError: (e) => toast.error(errorMessage(e, "Couldn't answer. The call may have ended.")),
    });
    router.push(`/meet/${ring.code}?autojoin=1`);
  };
  const onDecline = () => {
    tone.current?.stop();
    decline.mutate(ring.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't decline.")) });
  };

  return (
    <div role="alertdialog" aria-live="assertive" aria-label={`${ring.caller.display_name} is calling`}>
      <div className="mx-auto flex w-full max-w-sm flex-col gap-4 rounded-xl border border-border bg-surface-raised p-4 shadow-xl sm:w-96">
        <div className="flex items-center gap-3">
          <span className="relative flex">
            <AppAvatar name={ring.caller.display_name} src={ring.caller.avatar_url} size="lg" eager />
            <span className="absolute -right-1 -bottom-1 flex size-6 items-center justify-center rounded-pill bg-success text-success-fg motion-safe:animate-pulse">
              <BellRing className="size-3.5" aria-hidden />
            </span>
          </span>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <p className="truncate type-title-sm text-fg">{ring.caller.display_name}</p>
            <p className="truncate type-label text-fg-muted">{silent && !muted ? `${what} · sound blocked` : what}</p>
          </div>
          <IconButton
            label={muted ? "Turn ringtone on" : "Mute ringtone"}
            icon={muted ? <BellOff /> : <BellRing />}
            variant="ghost"
            size="sm"
            onClick={() => setMuted((m) => !m)}
          />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Button variant="danger" leftIcon={<PhoneOff />} onClick={onDecline} loading={decline.isPending}>
            Decline
          </Button>
          <Button variant="success" leftIcon={<Video />} onClick={onAccept} autoFocus>
            Accept
          </Button>
        </div>
      </div>
    </div>
  );
}
