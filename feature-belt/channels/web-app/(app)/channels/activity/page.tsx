import { Suspense } from "react";
import { ActivityScreen } from "@/features/channels";

export default function Page() {
  return (
    <Suspense>
      <ActivityScreen />
    </Suspense>
  );
}
