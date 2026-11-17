import { Suspense } from "react";
import { DraftsScreen } from "@/features/channels";

export default function Page() {
  return (
    <Suspense>
      <DraftsScreen />
    </Suspense>
  );
}
