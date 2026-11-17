"use client";

import { Dialog, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { ConnectionQuality, LocalAudioTrack, LocalVideoTrack, RemoteAudioTrack, RemoteVideoTrack, type Room } from "livekit-client";
import { useEffect, useRef, useState } from "react";

interface Row {
  key: string;
  who: string;
  kind: string;
  facts: string[];
}

const QUALITY: Record<ConnectionQuality, string> = {
  [ConnectionQuality.Excellent]: "Excellent connection",
  [ConnectionQuality.Good]: "Good connection",
  [ConnectionQuality.Poor]: "Weak connection",
  [ConnectionQuality.Lost]: "Connection lost",
  [ConnectionQuality.Unknown]: "Connection unknown",
};

const kbps = (bits: number) => (bits >= 1_000_000 ? `${(bits / 1_000_000).toFixed(1)} Mbps` : `${Math.round(bits / 1000)} kbps`);
const ms = (s: number | undefined) => (s === undefined ? null : `${Math.round(s * 1000)} ms`);

/** What the connection is doing, from WebRTC stats every two seconds: your sending, what you receive. */
async function sample(room: Room, prev: Map<string, { bytes: number; at: number }>): Promise<Row[]> {
  const rows: Row[] = [];
  const rate = (key: string, bytes: number | undefined, at: number) => {
    if (bytes === undefined) return null;
    const before = prev.get(key);
    prev.set(key, { bytes, at });
    if (!before || at <= before.at) return null;
    return kbps(((bytes - before.bytes) * 8 * 1000) / (at - before.at));
  };
  const lp = room.localParticipant;
  rows.push({ key: "you", who: "You", kind: "Connection", facts: [QUALITY[lp.connectionQuality]] });
  for (const pub of lp.trackPublications.values()) {
    const t = pub.track;
    if (t instanceof LocalVideoTrack) {
      const layers = await t.getSenderStats();
      const top = [...layers].sort((a, b) => b.frameWidth * b.frameHeight - a.frameWidth * a.frameHeight)[0];
      const bytes = layers.reduce((n, l) => n + (l.bytesSent ?? 0), 0);
      const r = rate(`out:${pub.trackSid}`, bytes, Date.now());
      rows.push({
        key: pub.trackSid,
        who: "You",
        kind: pub.source === "screen_share" ? "Screen sent" : "Video sent",
        facts: [
          top ? `${top.frameWidth}×${top.frameHeight} · ${Math.round(top.framesPerSecond || 0)} fps` : "No frames yet",
          ...(r ? [r] : []),
          ...(top?.qualityLimitationReason && top.qualityLimitationReason !== "none" ? [`limited by ${top.qualityLimitationReason}`] : []),
          ...(ms(top?.roundTripTime) ? [`round trip ${ms(top?.roundTripTime)}`] : []),
        ],
      });
    } else if (t instanceof LocalAudioTrack) {
      const st = await t.getSenderStats();
      const r = rate(`out:${pub.trackSid}`, st?.bytesSent, Date.now());
      rows.push({
        key: pub.trackSid,
        who: "You",
        kind: "Audio sent",
        facts: [...(r ? [r] : []), ...(ms(st?.roundTripTime) ? [`round trip ${ms(st?.roundTripTime)}`] : []), ...(st?.packetsLost ? [`${st.packetsLost} packets lost`] : [])],
      });
    }
  }
  for (const p of room.remoteParticipants.values()) {
    const name = p.name || "Guest";
    const facts: string[] = [QUALITY[p.connectionQuality]];
    for (const pub of p.trackPublications.values()) {
      const t = pub.track;
      if (t instanceof RemoteVideoTrack) {
        const st = await t.getReceiverStats();
        const r = rate(`in:${pub.trackSid}`, st?.bytesReceived, Date.now());
        if (st) facts.push(`${pub.source === "screen_share" ? "screen" : "video"} ${st.frameWidth ?? 0}×${st.frameHeight ?? 0}${r ? ` · ${r}` : ""}${st.framesDropped ? ` · ${st.framesDropped} dropped` : ""}`);
      } else if (t instanceof RemoteAudioTrack) {
        const st = await t.getReceiverStats();
        if (st) facts.push(`audio jitter ${ms(st.jitter) ?? "–"}${st.packetsLost ? ` · ${st.packetsLost} lost` : ""}`);
      }
    }
    rows.push({ key: p.identity, who: name, kind: "Receiving", facts });
  }
  return rows;
}

export function StatsDialog({ room, open, onOpenChange }: { room: Room; open: boolean; onOpenChange: (o: boolean) => void }) {
  const [rows, setRows] = useState<Row[] | null>(null);
  const prev = useRef(new Map<string, { bytes: number; at: number }>());
  useEffect(() => {
    if (!open) return undefined;
    let live = true;
    const tick = () =>
      void sample(room, prev.current)
        .then((r) => live && setRows(r))
        .catch(() => undefined);
    tick();
    const t = setInterval(tick, 2000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [open, room]);
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Connection" size="lg" info="Live numbers from your browser's WebRTC stats, every two seconds.">
      {!rows ? (
        <SkeletonGroup label="Measuring" className="flex flex-col gap-2">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </SkeletonGroup>
      ) : (
        <dl className="flex flex-col divide-y divide-border">
          {rows.map((r) => (
            <div key={r.key} className="flex flex-col gap-1 py-3 sm:flex-row sm:gap-4">
              <dt className="w-48 shrink-0 type-label font-semibold text-fg">
                {r.who} <span className="font-normal text-fg-muted">· {r.kind}</span>
              </dt>
              <dd className="type-body tabular-nums text-fg-muted">{r.facts.join(" · ") || "—"}</dd>
            </div>
          ))}
        </dl>
      )}
    </Dialog>
  );
}
