import { Directory, Paths } from "expo-file-system";
import * as SecureStore from "expo-secure-store";

/**
 * Small persisted values in SecureStore (Keychain / Keystore). Reads and writes swallow
 * storage errors: persistence is a convenience, never a blocker. Keep values small
 * (under 2 KB); larger per-user data goes in `userDirectory()`.
 */
export async function readJson<T>(key: string): Promise<T | null> {
  try {
    const raw = await SecureStore.getItemAsync(key);
    return raw ? (JSON.parse(raw) as T) : null;
  } catch {
    return null;
  }
}

export async function writeJson(key: string, value: unknown): Promise<void> {
  try {
    if (value === null || value === undefined) await SecureStore.deleteItemAsync(key);
    else await SecureStore.setItemAsync(key, JSON.stringify(value));
  } catch {
    // Storage unavailable: the value lives for this session only.
  }
}

/** Device-level keys: they survive sign-out (appearance is the device's, not the account's). */
export const DEVICE_KEYS = {
  appearance: "scaffoldapp.appearance",
  palette: "scaffoldapp.palette",
  devIdentity: "scaffoldapp.dev-identity",
  pendingRoute: "scaffoldapp.pending-route",
} as const;

const USER_PREFIX = "scaffoldapp.user.";
const USER_INDEX = "scaffoldapp.user-keys";

/**
 * Per-user values (drafts, last-seen markers, offline state). Everything written here is
 * wiped on sign-out and account deletion, so one device never shows another account's data.
 */
export const userStorage = {
  get: <T>(name: string) => readJson<T>(USER_PREFIX + name),
  async set(name: string, value: unknown): Promise<void> {
    const keys = new Set((await readJson<string[]>(USER_INDEX)) ?? []);
    if (!keys.has(name)) {
      keys.add(name);
      await writeJson(USER_INDEX, [...keys]);
    }
    await writeJson(USER_PREFIX + name, value);
  },
  remove: (name: string) => writeJson(USER_PREFIX + name, null),
};

/** A directory for per-user files (offline caches, downloads); emptied on sign-out. */
export function userDirectory(): Directory {
  const dir = new Directory(Paths.document, "user");
  if (!dir.exists) dir.create({ idempotent: true, intermediates: true });
  return dir;
}

/** Remove every per-user value and file. Called by the sign-out flow. */
export async function clearUserStorage(): Promise<void> {
  const keys = (await readJson<string[]>(USER_INDEX)) ?? [];
  await Promise.all(keys.map((name) => writeJson(USER_PREFIX + name, null)));
  await writeJson(USER_INDEX, null);
  try {
    const dir = new Directory(Paths.document, "user");
    if (dir.exists) dir.delete();
  } catch {
    // Nothing stored, or the file system refused: nothing more to clear.
  }
}
