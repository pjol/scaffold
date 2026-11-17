"use client";

import { useEffect, useState } from "react";

/** A fetch in flight, done or failed; `retry` starts it again. */
export type Loaded<T> = { status: "loading" } | { status: "error"; error: string } | { status: "ready"; data: T };

/** Reads up to `max` bytes of a URL as UTF-8; `truncated` says the file is longer. Streams so a huge file isn't downloaded whole. */
export async function fetchText(url: string, max: number, signal: AbortSignal): Promise<{ text: string; truncated: boolean }> {
  const res = await fetch(url, { signal, credentials: "omit" });
  if (!res.ok) throw new Error(res.status === 403 || res.status === 404 ? "The link has expired or the file is gone." : `The server answered ${res.status}.`);
  const decoder = new TextDecoder("utf-8", { fatal: false });
  if (!res.body) {
    const buf = new Uint8Array(await res.arrayBuffer());
    return { text: decoder.decode(buf.subarray(0, max)), truncated: buf.length > max };
  }
  const reader = res.body.getReader();
  const parts: string[] = [];
  let read = 0;
  let truncated = false;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    const room = max - read;
    if (value.length > room) {
      parts.push(decoder.decode(value.subarray(0, room), { stream: true }));
      truncated = true;
      void reader.cancel();
      break;
    }
    read += value.length;
    parts.push(decoder.decode(value, { stream: true }));
  }
  parts.push(decoder.decode());
  return { text: parts.join(""), truncated };
}

/** Reads a whole URL as bytes, refusing more than `max`. */
export async function fetchBytes(url: string, max: number, signal: AbortSignal, init?: RequestInit): Promise<{ bytes: Uint8Array; status: number }> {
  const res = await fetch(url, { ...init, signal, credentials: "omit" });
  if (!res.ok) throw new Error(res.status === 403 || res.status === 404 ? "The link has expired or the file is gone." : `The server answered ${res.status}.`);
  const len = Number(res.headers.get("content-length") ?? "0");
  if (len > max) {
    void res.body?.cancel();
    throw new TooLarge();
  }
  const buf = new Uint8Array(await res.arrayBuffer());
  if (buf.length > max) throw new TooLarge();
  return { bytes: buf, status: res.status };
}

/** The file is bigger than a preview reads. */
export class TooLarge extends Error {
  constructor() {
    super("This file is too large to preview.");
  }
}

/** Runs `load(signal)` whenever `key` changes; aborts the previous run. */
export function useLoaded<T>(key: string | null, load: (signal: AbortSignal) => Promise<T>): Loaded<T> & { retry: () => void } {
  const [state, setState] = useState<Loaded<T>>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (key === null) return;
    const ctrl = new AbortController();
    setState({ status: "loading" });
    load(ctrl.signal).then(
      (data) => !ctrl.signal.aborted && setState({ status: "ready", data }),
      (e: unknown) => {
        if (ctrl.signal.aborted) return;
        setState({ status: "error", error: e instanceof TooLarge ? "too_large" : e instanceof Error && e.name !== "TypeError" ? e.message : "Check your connection and try again." });
      },
    );
    return () => ctrl.abort();
    // `load` is recreated each render; `key` is what changes the request.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, attempt]);
  return { ...state, retry: () => setAttempt((a) => a + 1) };
}
