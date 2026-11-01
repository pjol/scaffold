import type { Metadata } from "next";
import { LinkEditorScreen } from "@/features/calendar/LinkEditor";

export const metadata: Metadata = { title: "New booking link" };

export default function Page() {
  return <LinkEditorScreen />;
}
