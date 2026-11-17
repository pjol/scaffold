import type { Metadata } from "next";
import { AdminForumScreen } from "@/features/forum/AdminForumScreen";

export const metadata: Metadata = { title: "Forum" };

export default function Page() {
  return <AdminForumScreen />;
}
