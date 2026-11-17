import { Suspense } from "react";
import { LaterScreen } from "@/features/channels";

export default function Page() {
  return (
    <Suspense>
      <LaterScreen />
    </Suspense>
  );
}
