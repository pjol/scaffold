import type { NativeIntent } from "expo-router";
import { noteSystemPath } from "@/lib/pending-route";

/**
 * Every link the OS hands the app passes through here first. Links that arrive while a
 * gate (sign-in, consent) is closed are remembered and opened once it's passed; the path
 * itself is never rewritten.
 */
export const redirectSystemPath: NativeIntent["redirectSystemPath"] = ({ path }) => {
  noteSystemPath(path);
  return path;
};
