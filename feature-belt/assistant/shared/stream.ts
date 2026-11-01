import { ApiError, clientErrorMessage, parseErrorResponse } from "../api/errors";
import { normalizeBaseUrl } from "../api/http";
import type { AssistantStreamEvent } from "./types";

/**
 * Streaming a reply: `POST /v1/assistant/conversations/{id}/messages` (send) or `GET` with
 * `Accept: text/event-stream` (resume a running reply) read through `fetch` + a
 * ReadableStream reader. Web passes the browser's fetch; Expo passes `fetch` from
 * `expo/fetch` (React Native's global fetch can't stream, so without it the events arrive
 * when the reply ends). Decoding is done here because Hermes has no TextDecoder.
 */

/** The parts of a fetch Response the stream reader uses. */
export interface StreamResponse {
  ok: boolean;
  status: number;
  headers: { get(name: string): string | null };
  text(): Promise<string>;
  body?: { getReader(): { read(): Promise<{ done: boolean; value?: Uint8Array | undefined }>; cancel?(reason?: unknown): Promise<void> } } | null;
}

export type StreamFetch = (url: string, init: { method: string; headers: Record<string, string>; body?: string; signal?: AbortSignal }) => Promise<StreamResponse>;

export interface StreamAssistantOptions {
  fetch: StreamFetch;
  /** API origin (`useApi().http.baseUrl`). */
  baseUrl: string;
  token: string | null | undefined;
  /** `X-Client-Platform`. */
  platform: string;
  conversationId: string;
  /** Send this message; omit to resume the running reply. */
  text?: string;
  signal?: AbortSignal;
  onEvent: (event: AssistantStreamEvent) => void;
}

const EVENTS = new Set(["message", "delta", "tool_start", "tool_end", "error", "done"]);

/**
 * Runs one stream to its end. Rejects with an `ApiError` for a refused request (403
 * `ai_consent_required`, 429 `quota_exceeded`, 503 `assistant_unconfigured`…), for a
 * network failure, or `aborted` when `signal` fires. Aborting only stops reading: the
 * reply keeps running on the server and can be resumed.
 */
export async function streamAssistant(o: StreamAssistantOptions): Promise<void> {
  const url = `${normalizeBaseUrl(o.baseUrl)}/v1/assistant/conversations/${encodeURIComponent(o.conversationId)}/messages`;
  const headers: Record<string, string> = { Accept: "text/event-stream", "X-Client-Platform": o.platform };
  if (o.token) headers["Authorization"] = `Bearer ${o.token}`;
  const init: { method: string; headers: Record<string, string>; body?: string; signal?: AbortSignal } = { method: o.text === undefined ? "GET" : "POST", headers };
  if (o.text !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify({ text: o.text });
  }
  if (o.signal) init.signal = o.signal;

  const fail = (cause: unknown): ApiError =>
    o.signal?.aborted
      ? new ApiError({ status: 0, code: "aborted", message: clientErrorMessage("aborted"), cause })
      : new ApiError({ status: 0, code: "network_error", message: clientErrorMessage("network_error"), cause });

  let res: StreamResponse;
  try {
    res = await o.fetch(url, init);
  } catch (cause) {
    throw fail(cause);
  }
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw parseErrorResponse(res.status, text, (name) => res.headers.get(name));
  }

  const parser = createSseParser((name, data) => {
    if (!EVENTS.has(name)) return;
    try {
      o.onEvent({ event: name, data: JSON.parse(data) } as AssistantStreamEvent);
    } catch {
      // A malformed frame is skipped; the final `message` event carries the full state.
    }
  });
  const reader = res.body?.getReader?.();
  try {
    if (!reader) {
      parser.push(await res.text());
    } else {
      const decoder = createUtf8Decoder();
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        if (value) parser.push(decoder.decode(value));
      }
      parser.push(decoder.flush());
    }
    parser.end();
  } catch (cause) {
    void reader?.cancel?.().catch(() => undefined);
    throw fail(cause);
  }
}

/**
 * Incremental text/event-stream parser: `event:` and multi-line `data:` fields, comment
 * lines (heartbeats) skipped, LF or CRLF, one dispatch per blank line.
 */
export function createSseParser(onFrame: (event: string, data: string) => void) {
  let buffer = "";
  let event = "";
  let data: string[] = [];
  const dispatch = () => {
    if (data.length) onFrame(event || "message", data.join("\n"));
    event = "";
    data = [];
  };
  const line = (l: string) => {
    if (l === "") return dispatch();
    if (l.startsWith(":")) return undefined;
    const i = l.indexOf(":");
    const field = i < 0 ? l : l.slice(0, i);
    let value = i < 0 ? "" : l.slice(i + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    if (field === "event") event = value;
    else if (field === "data") data.push(value);
    return undefined;
  };
  return {
    push(text: string) {
      buffer += text;
      for (;;) {
        const nl = buffer.indexOf("\n");
        if (nl < 0) break;
        let l = buffer.slice(0, nl);
        buffer = buffer.slice(nl + 1);
        if (l.endsWith("\r")) l = l.slice(0, -1);
        line(l);
      }
    },
    /** End of stream: an unterminated frame is dropped, as the spec says. */
    end() {
      buffer = "";
      event = "";
      data = [];
    },
  };
}

/** Streaming UTF-8 decoder that keeps a multi-byte character split across chunks. */
export function createUtf8Decoder() {
  let pending: number[] = [];
  const out = (units: number[]) => {
    let s = "";
    for (let i = 0; i < units.length; i += 4096) s += String.fromCharCode(...units.slice(i, i + 4096));
    return s;
  };
  return {
    decode(chunk: Uint8Array): string {
      const bytes = pending.length ? [...pending, ...chunk] : Array.from(chunk);
      pending = [];
      const units: number[] = [];
      let i = 0;
      while (i < bytes.length) {
        const b = bytes[i] ?? 0;
        const need = b < 0x80 ? 0 : b >= 0xf0 ? 3 : b >= 0xe0 ? 2 : b >= 0xc0 ? 1 : -1;
        if (need < 0) {
          units.push(0xfffd);
          i++;
          continue;
        }
        if (i + need >= bytes.length && need > 0) {
          pending = bytes.slice(i);
          break;
        }
        let cp = need === 0 ? b : b & (0x3f >> need);
        let ok = true;
        for (let k = 1; k <= need; k++) {
          const c = bytes[i + k] ?? 0;
          if ((c & 0xc0) !== 0x80) {
            ok = false;
            break;
          }
          cp = (cp << 6) | (c & 0x3f);
        }
        if (!ok) {
          units.push(0xfffd);
          i++;
          continue;
        }
        i += need + 1;
        if (cp > 0xffff) {
          cp -= 0x10000;
          units.push(0xd800 + (cp >> 10), 0xdc00 + (cp & 0x3ff));
        } else {
          units.push(cp);
        }
      }
      return out(units);
    },
    flush(): string {
      const rest = pending.length ? "�" : "";
      pending = [];
      return rest;
    },
  };
}
