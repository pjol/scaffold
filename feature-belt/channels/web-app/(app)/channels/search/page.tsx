import { Suspense } from "react";
import { SearchScreen } from "@/features/channels";

export default function Page() {
  return (
    <Suspense>
      <SearchScreen />
    </Suspense>
  );
}
