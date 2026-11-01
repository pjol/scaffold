import Constants from "expo-constants";
import { DEVICE_KEYS, readJson, writeJson } from "./storage";

/**
 * Where the user was going when a gate (sign-in, consent) stopped them: a deep link such
 * as `scaffoldapp://invites/<token>` or an in-app push. `RootNavigator` opens it once the
 * gates are open. Only this app's own paths are kept; it expires after 30 minutes and
 * survives the app being killed while the user fetches a sign-in code.
 */
interface Pending {
  path: string;
  at: number;
}

const TTL_MS = 30 * 60_000;
/** Gate screens themselves are never resumed. */
const NOT_RESUMABLE = /^\/(sign-in|consent|legal)(\/|\?|$)/;

let pending: Pending | null = null;
let gateOpen = false;

function schemes(): string[] {
  const s = Constants.expoConfig?.scheme;
  return (Array.isArray(s) ? s : s ? [s] : []).map((x) => x.toLowerCase());
}

/** `/a/b?x=1` for this app's scheme URLs and plain paths; null for anything else (other apps, the web, dev-client links). */
export function toAppPath(input: string): string | null {
  let path = input.trim();
  const url = /^([a-z][a-z0-9+.-]*):\/\/(.*)$/i.exec(path);
  if (url) {
    if (!schemes().includes((url[1] ?? "").toLowerCase())) return null;
    // `scheme:///a` and `scheme://a` both mean `/a` (the host is the first segment).
    path = `/${(url[2] ?? "").replace(/^\/+/, "")}`;
  }
  const pathname = path.split(/[?#]/)[0] ?? "";
  if (!pathname.startsWith("/") || pathname.startsWith("//") || pathname.includes("\\") || pathname.split("/").includes("..")) return null;
  if (/^\/(expo-development-client|--)(\/|$)/.test(pathname)) return null;
  return path;
}

/** Remember `raw` as the place to resume (ignored unless it's a resumable app path). */
export function rememberRoute(raw: string): void {
  const path = toAppPath(raw);
  if (!path || path === "/" || NOT_RESUMABLE.test(path)) return;
  pending = { path, at: Date.now() };
  void writeJson(DEVICE_KEYS.pendingRoute, pending);
}

/** The pending path (if still fresh), cleared as it's read. */
export function takePendingRoute(): string | null {
  const path = pending && Date.now() - pending.at < TTL_MS ? pending.path : null;
  if (pending) {
    pending = null;
    void writeJson(DEVICE_KEYS.pendingRoute, null);
  }
  return path;
}

/** `RootNavigator` reports whether the user is past every gate. */
export function setGateOpen(open: boolean): void {
  gateOpen = open;
}

/** Every system link (from `app/+native-intent.tsx`); remembered only while a gate is closed. */
export function noteSystemPath(raw: string): void {
  if (!gateOpen) rememberRoute(raw);
}

/** Restore a path saved before the app was killed (call once at launch). */
export async function restorePendingRoute(): Promise<void> {
  const saved = await readJson<Pending>(DEVICE_KEYS.pendingRoute);
  if (!pending && saved && typeof saved.path === "string" && Date.now() - saved.at < TTL_MS) pending = saved;
}
