import { Suspense } from "react";
import { UnreadsScreen } from "@/features/channels";

export default function Page() {
  return (
    <Suspense>
      <UnreadsScreen />
    </Suspense>
  );
}
