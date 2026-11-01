"use client";

import { Button, ErrorState } from "@scaffoldapp/ui-web";
import Link from "next/link";
import { useEffect } from "react";
import { CenteredScreen } from "@/components/shell/CenteredScreen";

/** Crash screen for anything outside the shell's own boundary. */
export default function RootError({ error, retry }: { error: Error & { digest?: string }; retry: () => void }) {
  useEffect(() => {
    console.error(error);
  }, [error]);
  return (
    <CenteredScreen>
      <div className="flex flex-col items-center gap-4">
        <ErrorState title="Something went wrong" info={error.digest ? `Reference ${error.digest}` : error.message} onRetry={retry} />
        <Button asChild variant="ghost">
          <Link href="/">Go home</Link>
        </Button>
      </div>
    </CenteredScreen>
  );
}
