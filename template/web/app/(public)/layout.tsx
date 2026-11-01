import type { ReactNode } from "react";
import { PublicFrame } from "@/components/shell/PublicFrame";

export default function PublicLayout({ children }: { children: ReactNode }) {
  return <PublicFrame>{children}</PublicFrame>;
}
