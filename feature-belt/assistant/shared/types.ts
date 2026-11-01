/** Assistant DTOs: snake_case, mirroring backend/internal/assistant (store.go, assistant.go, turns.go). */

/** The AI provider the server is configured with. `label` names the company content is sent to. */
export interface AssistantProvider {
  name: "anthropic" | "openai" | "mock" | (string & {});
  label: string;
}

/** Whether the user has allowed sending their content to the provider (Apple 5.1.2(i)). */
export interface AssistantConsent {
  required: boolean;
  provider: string;
  accepted_at: string | null;
}

/** Today's usage (UTC day) against the daily limits. A limit of 0 means unlimited. */
export interface AssistantUsage {
  day: string;
  messages: number;
  message_limit: number;
  tokens: number;
  token_limit: number;
  /** Estimated cost, decimal string in USD. */
  cost_usd: string;
  unlimited: boolean;
  resets_at: string;
}

/** `GET /v1/assistant/status`. `provider` is null when the server has no provider configured. */
export interface AssistantStatus {
  available: boolean;
  provider: AssistantProvider | null;
  model: string;
  /** Development mock: replies are scripted. Show it in the UI. */
  mock: boolean;
  allow_writes: boolean;
  max_message_chars: number;
  consent: AssistantConsent;
  usage: AssistantUsage;
}

export interface AssistantConversation {
  id: string;
  title: string;
  /** A reply is streaming; resume it with `streamAssistant` (no `text`). */
  running: boolean;
  created_at: string;
  updated_at: string;
  last_message_at: string;
}

export interface AssistantDisplayPart {
  type: "text";
  text: string;
}

export type AssistantToolStatus = "running" | "ok" | "error";

/** A tool call as shown under a reply. */
export interface AssistantActivity {
  call_id: string;
  name: string;
  status: AssistantToolStatus;
  summary: string;
  input?: unknown;
  started_at: string;
  duration_ms: number;
}

export interface AssistantMessageError {
  code: string;
  message: string;
  retryable: boolean;
}

export interface AssistantTurnUsage {
  input_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  output_tokens: number;
  rounds: number;
  estimated: boolean;
}

export type AssistantMessageStatus = "streaming" | "complete" | "cancelled" | "error";

export interface AssistantMessage {
  id: string;
  conversation_id: string;
  role: "user" | "assistant";
  parts: AssistantDisplayPart[];
  activity: AssistantActivity[];
  provider: string;
  model: string;
  status: AssistantMessageStatus;
  /** end_turn | max_tokens | refusal | blocked | cancelled | error */
  stop_reason: string;
  error: AssistantMessageError | null;
  usage: AssistantTurnUsage | null;
  cost_usd: string | null;
  /** Taken down by staff after a report; `parts` is empty. */
  removed: boolean;
  created_at: string;
  completed_at: string | null;
}

/** `GET /v1/admin/assistant/usage`: one day across all users. */
export interface AssistantDailyTotals {
  day: string;
  users: number;
  messages: number;
  input_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  output_tokens: number;
  cost_usd: string;
}

/** Server-sent events of `POST/GET /v1/assistant/conversations/{id}/messages`. */
export type AssistantStreamEvent =
  | { event: "message"; data: { message: AssistantMessage } }
  | { event: "delta"; data: { message_id: string; text: string } }
  | { event: "tool_start"; data: { message_id: string; call_id: string; name: string; input: unknown; started_at: string } }
  | { event: "tool_end"; data: { message_id: string; call_id: string; name: string; status: "ok" | "error"; summary: string; duration_ms: number } }
  | { event: "error"; data: { message_id: string; code: string; message: string; retryable: boolean } }
  | { event: "done"; data: { message_id: string | null; status: AssistantMessageStatus; conversation: AssistantConversation | null } };

export interface AssistantConversationListParams {
  q?: string;
  cursor?: string;
  limit?: number;
}
