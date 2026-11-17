"use client";

import { errorMessage, formatCallsCountdown, useAskCallsHelp, type CallsMyBreakout, type CallsRoomMeta } from "@scaffoldapp/shared";
import { Banner, Button, Dialog, cn, toast } from "@scaffoldapp/ui-web";
import { DoorOpen, LifeBuoy, LogOut } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useCountdown } from "./countdown";
import { useRoomTarget } from "./session";

const AUTO_JOIN_SECONDS = 10;

/**
 * In the main room while breakout rooms are open: your room, with a 10-second automatic
 * move (Join now / Not now); or, when people choose, the list of rooms. Dismissed, it
 * leaves a banner to join later.
 */
export function BreakoutPrompt({ mine, joining, onJoin }: { mine: CallsMyBreakout; joining: boolean; onJoin: (roomId?: string) => void }) {
  const key = `${mine.id}:${mine.room?.id ?? "choose"}`;
  const [dismissed, setDismissed] = useState<string | null>(null);
  const [deadline, setDeadline] = useState<string | null>(null);
  const open = dismissed !== key && mine.status === "open" && (!!mine.room || mine.allow_choose);
  useEffect(() => {
    setDeadline(mine.room && mine.status === "open" ? new Date(Date.now() + AUTO_JOIN_SECONDS * 1000).toISOString() : null);
  }, [key, mine.room, mine.status]);
  const left = useCountdown(open && mine.room ? deadline : null);
  const fired = useRef<string | null>(null); // the automatic move happens once per assignment
  useEffect(() => {
    if (open && mine.room && left === 0 && !joining && fired.current !== key) {
      fired.current = key;
      onJoin();
    }
  }, [left, open, mine.room, joining, onJoin, key]);

  if (mine.status !== "open" || (!mine.room && !mine.allow_choose)) return null;
  if (!open) {
    return (
      <Banner
        tone="info"
        icon={<DoorOpen aria-hidden />}
        action={
          <Button size="sm" variant="soft" loading={joining} onClick={() => (mine.room ? onJoin() : setDismissed(null))}>
            {mine.room ? `Join ${mine.room.name}` : "Choose a room"}
          </Button>
        }
      >
        Breakout rooms are open.
      </Banner>
    );
  }
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && setDismissed(key)}
      title={mine.room ? `Join ${mine.room.name}?` : "Choose a breakout room"}
      size="sm"
      info="You can come back to the main room at any time."
      footer={
        mine.room ? (
          <>
            <Button variant="ghost" onClick={() => setDismissed(key)}>
              Not now
            </Button>
            <Button loading={joining} onClick={() => onJoin()}>
              Join now{left !== null && left > 0 ? ` (${left})` : ""}
            </Button>
          </>
        ) : (
          <Button variant="ghost" onClick={() => setDismissed(key)}>
            Not now
          </Button>
        )
      }
    >
      {mine.room ? (
        <p className="type-body text-fg">A host moved you to {mine.room.name}. You join it automatically in {left ?? AUTO_JOIN_SECONDS} seconds.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {mine.rooms.map((r) => (
            <li key={r.id}>
              <Button variant="secondary" className="w-full justify-start" disabled={joining} onClick={() => onJoin(r.id)}>
                {r.name}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  );
}

/** In a breakout room: its name, time left or the closing countdown, help and the way back. */
export function BreakoutBar({ meta, returning, onReturn }: { meta: CallsRoomMeta; returning: boolean; onReturn: () => void }) {
  const target = useRoomTarget();
  const help = useAskCallsHelp(target);
  const b = meta.breakout;
  const closing = b?.status === "closing";
  const left = useCountdown(closing ? b?.closing_at : b?.ends_at);
  if (!b?.room) return null;
  // Not a Banner: the actions wrap under the room's name and time on phones.
  return (
    <div className={cn("flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md px-3 py-2", closing ? "bg-warning-soft text-warning-strong" : "bg-primary-soft text-fg")} role="status">
      <span className="flex min-w-0 flex-1 basis-48 items-center gap-2 type-label">
        <DoorOpen className="size-4 shrink-0" aria-hidden />
        <span role="timer" className="min-w-0">
          <span className="font-semibold">{b.room.name}</span>
          {closing ? ` · back to the main room in ${formatCallsCountdown(left ?? 0)}` : left !== null ? ` · ${formatCallsCountdown(left)} left` : ""}
        </span>
      </span>
      <span className="flex flex-wrap gap-2">
        {!closing && (
          <Button
            size="sm"
            variant="secondary"
            leftIcon={<LifeBuoy />}
            loading={help.isPending}
            onClick={() => help.mutate(undefined, { onSuccess: () => toast.success("The hosts know you need help"), onError: (e) => toast.error(errorMessage(e, "Couldn't reach the hosts.")) })}
          >
            Ask for help
          </Button>
        )}
        <Button size="sm" variant="secondary" leftIcon={<LogOut />} loading={returning} onClick={onReturn}>
          Main room
        </Button>
      </span>
    </div>
  );
}
