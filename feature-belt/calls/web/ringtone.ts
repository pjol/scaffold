/**
 * The incoming-call ringtone, synthesised with Web Audio (no audio file to ship or
 * license): two soft notes every 2.5 s until stopped. Browsers only allow sound after the
 * person has interacted with the page; when the context can't start, `start` resolves
 * false and the overlay shows the call silently.
 */
export interface Ringtone {
  start(): Promise<boolean>;
  stop(): void;
}

type AudioContextCtor = typeof AudioContext;

export function createRingtone(): Ringtone {
  let ctx: AudioContext | null = null;
  let timer: ReturnType<typeof setInterval> | null = null;

  const note = (at: number, freq: number, length: number) => {
    if (!ctx) return;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = "sine";
    osc.frequency.value = freq;
    gain.gain.setValueAtTime(0, at);
    gain.gain.linearRampToValueAtTime(0.18, at + 0.02);
    gain.gain.exponentialRampToValueAtTime(0.0001, at + length);
    osc.connect(gain).connect(ctx.destination);
    osc.start(at);
    osc.stop(at + length + 0.05);
  };

  const ring = () => {
    if (!ctx) return;
    const t = ctx.currentTime + 0.02;
    note(t, 659.25, 0.45); // E5
    note(t + 0.32, 523.25, 0.7); // C5
  };

  return {
    async start() {
      if (timer) return true;
      const Ctor = (window.AudioContext ?? (window as unknown as { webkitAudioContext?: AudioContextCtor }).webkitAudioContext) as AudioContextCtor | undefined;
      if (!Ctor) return false;
      try {
        ctx = new Ctor();
        if (ctx.state === "suspended") await ctx.resume();
        if (ctx.state !== "running") {
          void ctx.close();
          ctx = null;
          return false;
        }
      } catch {
        ctx = null;
        return false;
      }
      ring();
      timer = setInterval(ring, 2500);
      return true;
    },
    stop() {
      if (timer) clearInterval(timer);
      timer = null;
      if (ctx) void ctx.close();
      ctx = null;
    },
  };
}
