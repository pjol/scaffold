import type { Metadata } from "next";
import { Suspense } from "react";
import { UsersScreen } from "@/screens/admin/UsersScreen";

export const metadata: Metadata = { title: "Users" };

export default function UsersPage() {
  return (
    <Suspense>
      <UsersScreen />
    </Suspense>
  );
}
