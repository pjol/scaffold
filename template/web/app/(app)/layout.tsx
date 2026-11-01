import type { Metadata } from "next";
import type { ReactNode } from "react";
import { AppFrame } from "@/components/shell/AppFrame";

/** Signed-in app: auth gate, consent gate and the shell (src/components/shell/AppFrame.tsx). */
export const metadata: Metadata = { robots: { index: false, follow: false } };

export default function AppLayout({ children }: { children: ReactNode }) {
  return <AppFrame>{children}</AppFrame>;
}
