import type { Metadata } from "next";
import { FollowingScreen } from "@/features/social/FollowingScreen";

export const metadata: Metadata = { title: "Following" };

export default function Page() {
  return <FollowingScreen />;
}
