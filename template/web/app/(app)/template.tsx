"use client";

import { PageTransition } from "@scaffoldapp/ui-web";
import type { ReactNode } from "react";

/** Re-mounts on navigation between top-level sections: each rises in on the gentle spring. */
export default function AppTemplate({ children }: { children: ReactNode }) {
  return <PageTransition className="flex flex-1 flex-col">{children}</PageTransition>;
}
