"use client";

import { Button, ErrorState } from "@scaffoldapp/ui-web";
import Link from "next/link";
import { useEffect } from "react";

/** In-shell error boundary: the navigation stays while the screen recovers. */
export default function AppError({ error, retry }: { error: Error & { digest?: string }; retry: () => void }) {
  useEffect(() => {
    console.error(error);
  }, [error]);
  return (
    <div className="mx-auto flex w-full max-w-content flex-col items-center gap-4 px-gutter py-section">
      <ErrorState title="This screen didn't load" info={error.digest ? `Reference ${error.digest}` : error.message} onRetry={retry} />
      <Button asChild variant="ghost">
        <Link href="/">Go home</Link>
      </Button>
    </div>
  );
}
