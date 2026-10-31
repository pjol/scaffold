/** `"<kind>:<id>"`, e.g. `topic("chat", id)`. Matches Go `realtime.Topic`. */
export const topic = (kind: string, id: string): string => `${kind}:${id}`;
/** `user:<id>`: the server subscribes every connection to its own; never sent by clients. */
export const userTopic = (userId: string): string => topic("user", userId);
/** Staff feed (support and admin only). */
export const ADMIN_LIVE_TOPIC = "admin:live";

/** The id in a `"<kind>:<id>"` topic, or null when the topic is another kind. */
export function topicId(value: string | null | undefined, kind: string): string | null {
  const prefix = `${kind}:`;
  return value && value.startsWith(prefix) ? value.slice(prefix.length) : null;
}

/** `user:` topics are server-managed; never send subscribe/unsubscribe for them. */
export function isServerManagedTopic(value: string): boolean {
  return value.startsWith("user:");
}

/**
 * The WebSocket endpoint for an API origin:
 * `https://api.example.com` → `wss://api.example.com/v1/ws`.
 */
export function toWebSocketUrl(apiBaseUrl: string): string {
  let url = apiBaseUrl.trim().replace(/\/+$/, "");
  if (url.endsWith("/v1")) url = url.slice(0, -3);
  if (url.startsWith("https://")) url = `wss://${url.slice(8)}`;
  else if (url.startsWith("http://")) url = `ws://${url.slice(7)}`;
  return `${url}/v1/ws`;
}
