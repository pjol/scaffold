import type {
  Connector,
  ConnectorCheckResult,
  ConnectorDisconnected,
  ConnectorPlatform,
  ConnectorPlatformCapability,
  ConnectorPlatformConnection,
  ConnectorPlatformDisconnected,
} from "./types";

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

// ---- connected platforms --------------------------------------------------------------

/** The toast after disconnecting a platform account. */
export function connectorPlatformDisconnectedMessage(platform: string, account: string, r: ConnectorPlatformDisconnected): string {
  switch (r.revocation) {
    case "revoked":
      return `Disconnected ${account}. ${platform} revoked access.`;
    case "pending":
      return `Disconnected ${account}. ${platform} will be told to revoke access shortly.`;
    case "manual":
      return `Disconnected ${account}. Revoke the token in ${platform} if nothing else uses it.`;
    default:
      return `Disconnected ${account}. Remove this app's access in your ${platform} account settings too.`;
  }
}

const moduleLabels: Record<string, string> = { crm: "CRM", repos: "Repositories", docs: "Docs", files: "Files", calendar: "Calendar", calls: "Calls", channels: "Channels", search: "Search", assistant: "Assistant" };

/** Names a module for the connections screens; register an app's own at module load. */
export function registerConnectorModuleLabel(module: string, label: string): void {
  moduleLabels[module] = label;
}

/** A feature's name from its module id: "files" → "Files", "crm" → "CRM". */
export function connectorModuleLabel(module: string): string {
  return moduleLabels[module] ?? (module ? module.charAt(0).toUpperCase() + module.slice(1).replace(/[_-]+/g, " ") : module);
}

/** "a personal access token", "an internal integration token": a token kind with its article. */
export function connectorTokenLabel(label: string): string {
  const l = label.toLowerCase();
  return `${/^[aeiou]/.test(l) ? "an" : "a"} ${l}`;
}

/** Capabilities grouped by the feature that declared them, in the order they came. */
export function connectorCapabilitiesByFeature(caps: ConnectorPlatformCapability[]): { module: string; label: string; capabilities: ConnectorPlatformCapability[] }[] {
  const out: { module: string; label: string; capabilities: ConnectorPlatformCapability[] }[] = [];
  for (const c of caps) {
    let g = out.find((x) => x.module === c.module);
    if (!g) {
      g = { module: c.module, label: connectorModuleLabel(c.module), capabilities: [] };
      out.push(g);
    }
    g.capabilities.push(c);
  }
  return out;
}

/** The state of one capability on one account. */
export function connectorCapabilityState(conn: ConnectorPlatformConnection, key: string) {
  return conn.capabilities.find((c) => c.key === key)?.state ?? "off";
}

/** Whether a platform can be connected here at all (sign-in set up, or personal tokens). */
export function connectorPlatformAvailable(p: Pick<ConnectorPlatform, "oauth" | "token">): boolean {
  return p.oauth || !!p.token;
}

/** What went wrong, from `?connect_error=` after a platform sign-in. */
export function connectorPlatformErrorMessage(platform: string, code: string | null | undefined): string {
  switch (code) {
    case "cancelled":
      return `Connecting ${platform} was cancelled.`;
    case "expired":
      return "That sign-in took too long or was already used. Connect again.";
    case "exchange_failed":
      return `${platform} didn't finish the sign-in. Try again.`;
    case "provider_error":
      return `${platform} refused the sign-in. Try again, or ask an admin to check the platform's setup.`;
    default:
      return "Connecting didn't finish. Try again.";
  }
}
