/**
 * Per-browser conveniences: chosen devices and camera/mic defaults, a guest's name, and a
 * guest's lobby key for the tab they knocked from. Every access is guarded: storage can be
 * missing or throw (private windows), and the call works without it.
 */

const DEVICES_KEY = "scaffoldapp.calls.devices";
const GUEST_NAME_KEY = "scaffoldapp.calls.guest-name";
const guestKey = (code: string) => `scaffoldapp.calls.guest.${code}`;

export interface CallsDevicePrefs {
  audioInput?: string;
  videoInput?: string;
  audioOutput?: string;
  /** Join with the camera on. */
  camera: boolean;
  /** Join with the microphone on. */
  mic: boolean;
  /** Background blur on the camera. */
  blur: boolean;
}

const DEFAULT_PREFS: CallsDevicePrefs = { camera: true, mic: true, blur: false };

function read<T>(store: () => Storage, key: string): T | null {
  try {
    const raw = store().getItem(key);
    return raw ? (JSON.parse(raw) as T) : null;
  } catch {
    return null;
  }
}

function write(store: () => Storage, key: string, value: unknown) {
  try {
    if (value === null) store().removeItem(key);
    else store().setItem(key, JSON.stringify(value));
  } catch {
    // Storage is a convenience only.
  }
}

const local = () => window.localStorage;
const session = () => window.sessionStorage;

export function readDevicePrefs(): CallsDevicePrefs {
  return { ...DEFAULT_PREFS, ...(read<Partial<CallsDevicePrefs>>(local, DEVICES_KEY) ?? {}) };
}

export function writeDevicePrefs(prefs: CallsDevicePrefs) {
  write(local, DEVICES_KEY, prefs);
}

export function readGuestName(): string {
  return read<string>(local, GUEST_NAME_KEY) ?? "";
}

export function writeGuestName(name: string) {
  write(local, GUEST_NAME_KEY, name);
}

export interface GuestPass {
  lobbyId: string;
  key: string;
  name: string;
}

/** A guest's lobby entry for one meeting, kept for this tab only. */
export function readGuestPass(code: string): GuestPass | null {
  return read<GuestPass>(session, guestKey(code));
}

export function writeGuestPass(code: string, pass: GuestPass | null) {
  write(session, guestKey(code), pass);
}
