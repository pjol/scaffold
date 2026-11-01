import type { Connector, ConnectorCheckResult, ConnectorDisconnected } from "./types";

/**
 * The agent-scope key of one external service, as the backend sets it on its tools
 * (`connectors.FeatureKey`): pass it as `feature` to the resource menu so the agents
 * feature's "AI agent access" scopes this service on its own.
 */
export function connectorFeatureKey(connectorId: string): string {
  return `connectors:${connectorId}`;
}

/** What went wrong, from `?connect_error=` on the return URL. */
export function connectorConnectErrorMessage(code: string | null | undefined): string {
  switch (code) {
    case "cancelled":
      return "Connecting was cancelled.";
    case "expired":
      return "That sign-in took too long or was already used. Connect again.";
    case "issuer_mismatch":
      return "The sign-in came back from an unexpected server, so it was refused. Connect again.";
    case "exchange_failed":
      return "The service didn't finish the sign-in. Try again.";
    case "provider_error":
      return "The service refused the sign-in. Try again or ask an admin to check the connector.";
    default:
      return "Connecting didn't finish. Try again.";
  }
}

export type ConnectorState = "connected" | "needs_reauth" | "unavailable" | "not_connected";

/** One state for a connector row. */
export function connectorState(c: Pick<Connector, "available" | "connection">): ConnectorState {
  if (!c.connection) return "not_connected";
  if (!c.available) return "unavailable";
  return c.connection.status === "needs_reauth" ? "needs_reauth" : "connected";
}

export function connectorStateLabel(state: ConnectorState): string {
  switch (state) {
    case "connected":
      return "Connected";
    case "needs_reauth":
      return "Sign in again";
    case "unavailable":
      return "Turned off";
    default:
      return "Not connected";
  }
}

/** The toast after a disconnect. */
export function connectorDisconnectedMessage(name: string, r: ConnectorDisconnected): string {
  switch (r.revocation) {
    case "revoked":
      return `Disconnected from ${name}`;
    case "pending":
      return `Disconnected. ${name} will be told to revoke access shortly.`;
    default:
      return `Disconnected. Remove the app's access in ${name}'s settings too.`;
  }
}

/** How connecting will register with the server, for the admin check. */
export function connectorRegistrationLabel(r: ConnectorCheckResult["registration"]): string {
  switch (r) {
    case "preregistered":
      return "Uses your client ID";
    case "metadata_document":
      return "Client ID metadata document";
    case "dynamic":
      return "Registers itself (dynamic client registration)";
    case "needs_client_id":
      return "Needs a client ID registered with the service";
    case "none":
      return "Users paste a token";
    default:
      return "Unknown";
  }
}
