import type { Metadata } from "next";
import { Suspense } from "react";
import { DeleteAccountScreen } from "@/screens/public/DeleteAccountScreen";

export const metadata: Metadata = { title: "Delete your account" };

export default function DeleteAccountPage() {
  return (
    <Suspense>
      <DeleteAccountScreen />
    </Suspense>
  );
}
