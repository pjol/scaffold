/** Why the browser refused a camera or microphone, in words the person can act on. */
export type CallsMediaProblem = "denied" | "missing" | "busy" | "insecure" | "other";

export function mediaProblem(error: unknown): CallsMediaProblem {
  if (typeof window !== "undefined" && !window.isSecureContext) return "insecure";
  const name = error instanceof Error ? error.name : typeof error === "object" && error && "name" in error ? String((error as { name: unknown }).name) : "";
  const message = error instanceof Error ? error.message.toLowerCase() : "";
  if (name === "NotAllowedError" || name === "SecurityError" || message.includes("permission")) return "denied";
  if (name === "NotFoundError" || name === "OverconstrainedError" || message.includes("not found")) return "missing";
  if (name === "NotReadableError" || name === "AbortError" || message.includes("in use")) return "busy";
  return "other";
}

export function mediaProblemText(kind: "camera" | "microphone", problem: CallsMediaProblem): string {
  const thing = kind === "camera" ? "camera" : "microphone";
  switch (problem) {
    case "denied":
      return `Your browser blocked the ${thing}. Allow it in the site settings, then try again.`;
    case "missing":
      return `No ${thing} found. Connect one and try again.`;
    case "busy":
      return `Another app is using the ${thing}. Close it and try again.`;
    case "insecure":
      return `The ${thing} only works on a secure (https) connection.`;
    default:
      return `Couldn't start the ${thing}. Try again.`;
  }
}

/** Whether this platform uses ⌘ for shortcuts. */
export function isMac(): boolean {
  return typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
}

/** "⌘ D" / "Ctrl D", for tooltips. */
export function shortcutLabel(key: string, alt = false): string {
  const mod = isMac() ? "⌘" : "Ctrl";
  return alt ? `${mod} ${isMac() ? "⌥" : "Alt"} ${key}` : `${mod} ${key}`;
}
