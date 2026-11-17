"use client";

import { useDataChannel, useLocalParticipant } from "@livekit/components-react";
import { CALLS_CAPTION_TOPIC, callsEndpoints, decodeCallsCaption, encodeCallsCaption, useEndpoints, type CallsRoomTarget } from "@scaffoldapp/shared";
import { Button, Dialog, toast } from "@scaffoldapp/ui-web";
import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Captions without a server: each speaker's own browser turns their speech into text
 * (the Web Speech API) and sends it to the room over the data channel; everyone who
 * turned captions on shows the lines. In Chrome and Edge the browser sends the audio to
 * its vendor's speech service, so it is off until the workspace allows it and each person
 * chooses to caption their own speech. While a host saves the transcript, each speaker's
 * final lines also go to the server (their own speech only).
 */

interface SpeechAlternative {
  transcript: string;
}
interface SpeechResult {
  readonly isFinal: boolean;
  readonly length: number;
  [index: number]: SpeechAlternative;
}
interface SpeechResultEvent {
  resultIndex: number;
  results: { readonly length: number; [index: number]: SpeechResult };
}
interface SpeechRecognitionLike {
  continuous: boolean;
  interimResults: boolean;
  lang: string;
  onresult: ((e: SpeechResultEvent) => void) | null;
  onerror: ((e: { error: string }) => void) | null;
  onend: (() => void) | null;
  start(): void;
  stop(): void;
  abort(): void;
}
type SpeechRecognitionCtor = new () => SpeechRecognitionLike;

function speechCtor(): SpeechRecognitionCtor | null {
  if (typeof window === "undefined") return null;
  const w = window as unknown as { SpeechRecognition?: SpeechRecognitionCtor; webkitSpeechRecognition?: SpeechRecognitionCtor };
  return w.SpeechRecognition ?? w.webkitSpeechRecognition ?? null;
}

/** Whether this browser can caption your own speech. */
export function canCaptionOwnSpeech(): boolean {
  return speechCtor() !== null;
}

/** One speaker's latest caption line. */
export interface CaptionLine {
  identity: string;
  name: string;
  text: string;
  at: number;
}

const SHOW_MS = 6_000;

/**
 * Receives everyone's captions (when `show`), and, when `speak` is on and your microphone
 * is on, captions your speech and sends it. Returns the lines to show.
 */
export function useCaptions({ show, speak, transcribing, target }: { show: boolean; speak: boolean; transcribing: boolean; target: CallsRoomTarget }): CaptionLine[] {
  const [lines, setLines] = useState<Record<string, CaptionLine>>({});
  const { localParticipant, isMicrophoneEnabled } = useLocalParticipant();
  const api = useEndpoints(callsEndpoints);
  const put = useCallback((identity: string, name: string, text: string) => {
    setLines((prev) => ({ ...prev, [identity]: { identity, name, text, at: Date.now() } }));
  }, []);

  useDataChannel(CALLS_CAPTION_TOPIC, (msg) => {
    if (!show || !msg.from) return; // captions only come from participants (attributed by the server)
    const c = decodeCallsCaption(msg.payload);
    if (c?.text) put(msg.from.identity, msg.from.name || "Someone", c.text);
  });

  // Drop lines nobody added to for a while.
  useEffect(() => {
    if (!show) return undefined;
    const t = setInterval(() => {
      const cutoff = Date.now() - SHOW_MS;
      setLines((prev) => {
        const next = Object.fromEntries(Object.entries(prev).filter(([, l]) => l.at > cutoff));
        return Object.keys(next).length === Object.keys(prev).length ? prev : next;
      });
    }, 1_000);
    return () => clearInterval(t);
  }, [show]);

  // Your own speech.
  const pending = useRef<{ text: string; spoken_at: string }[]>([]);
  const latest = useRef({ show, transcribing, target });
  latest.current = { show, transcribing, target };
  const active = speak && isMicrophoneEnabled;
  useEffect(() => {
    const Ctor = speechCtor();
    if (!active || !Ctor) return undefined;
    const rec = new Ctor();
    rec.continuous = true;
    rec.interimResults = true;
    rec.lang = typeof navigator !== "undefined" ? navigator.language || "en-US" : "en-US";
    let running = true;
    let seq = 0;
    let lastSent = 0;
    const send = (text: string, final: boolean) => {
      const now = Date.now();
      if (!final && now - lastSent < 250) return; // a few interim updates a second
      lastSent = now;
      void localParticipant.publishData(encodeCallsCaption({ id: seq, text, final }), { reliable: final, topic: CALLS_CAPTION_TOPIC }).catch(() => undefined);
      if (latest.current.show) put(localParticipant.identity, "You", text);
    };
    rec.onresult = (e) => {
      for (let i = e.resultIndex; i < e.results.length; i++) {
        const r = e.results[i];
        const text = r?.[0]?.transcript.trim() ?? "";
        if (!r || !text) continue;
        send(text, r.isFinal);
        if (r.isFinal) {
          seq++;
          if (latest.current.transcribing) pending.current.push({ text, spoken_at: new Date().toISOString() });
        }
      }
    };
    rec.onerror = (e) => {
      if (e.error === "not-allowed" || e.error === "service-not-allowed") {
        running = false;
        toast.error("Your browser blocked speech recognition, so your speech isn't captioned.");
      }
    };
    rec.onend = () => {
      if (running) {
        try {
          rec.start(); // the browser stops after silence; keep listening
        } catch {
          // already started
        }
      }
    };
    try {
      rec.start();
    } catch {
      running = false;
    }
    return () => {
      running = false;
      rec.onend = null;
      rec.abort();
    };
  }, [active, localParticipant, put]);

  // Saved transcript: send your final lines every few seconds.
  useEffect(() => {
    if (!transcribing) {
      pending.current = [];
      return undefined;
    }
    const flush = () => {
      const batch = pending.current.splice(0, 20);
      if (batch.length === 0) return;
      api
        .room(latest.current.target)
        .transcript(batch)
        .catch(() => undefined); // the transcript may have just been turned off
    };
    const t = setInterval(flush, 3_000);
    return () => {
      clearInterval(t);
      flush();
    };
  }, [transcribing, api]);

  return Object.values(lines).sort((a, b) => a.at - b.at).slice(-3);
}

/** The latest lines at the bottom of the stage. */
export function CaptionsOverlay({ lines }: { lines: CaptionLine[] }) {
  if (lines.length === 0) return null;
  return (
    <div className="pointer-events-none absolute inset-x-0 bottom-4 flex justify-center px-4" aria-live="polite" aria-label="Captions">
      <div className="flex max-w-3xl flex-col gap-1 rounded-lg bg-ink-950/80 px-4 py-3 text-ink-50">
        {lines.map((l) => (
          <p key={l.identity} className="type-body">
            <span className="font-semibold text-ink-300">{l.name}: </span>
            {l.text}
          </p>
        ))}
      </div>
    </div>
  );
}

/** Asks once per call whether to caption your own speech, saying where the audio goes. */
export function CaptionChoiceDialog({ open, onChoose }: { open: boolean; onChoose: (speak: boolean | null) => void }) {
  const supported = canCaptionOwnSpeech();
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onChoose(null)}
      title="Caption your own speech too?"
      size="sm"
      info="People see captions only for speakers who caption themselves. Your browser turns your speech into text: Chrome and Edge send the audio to Google or Microsoft to do it."
      footer={
        <>
          <Button variant="ghost" onClick={() => onChoose(false)}>
            Only show captions
          </Button>
          <Button disabled={!supported} onClick={() => onChoose(true)}>
            Caption my speech
          </Button>
        </>
      }
    >
      <p className="type-body text-fg">
        {supported ? "Your captions go to everyone in the call while your microphone is on." : "This browser can't turn speech into text. You'll still see other people's captions."}
      </p>
    </Dialog>
  );
}
