"use client";

import { errorMessage, useJoinEvent } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, Spinner } from "@scaffoldapp/ui-web";
import { Unlink } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useRef } from "react";

/** /events/join?code=…: joins the invite list with an invite link code, then opens the event. */
export function JoinEventScreen() {
  const code = useSearchParams().get("code")?.trim() ?? "";
  const router = useRouter();
  const join = useJoinEvent();
  const started = useRef(false);

  useEffect(() => {
    if (!code || started.current) return;
    started.current = true;
    join.mutate(code, { onSuccess: (event) => router.replace(`/events/${event.id}`) });
  }, [code, join, router]);

  const failed = !code || join.isError;
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      {failed ? (
        <Card className="max-w-form">
          <EmptyState
            icon={<Unlink />}
            title="That invite link doesn't work anymore"
            info={join.isError ? errorMessage(join.error, "Ask the host for a new link.") : "Ask the host for a new link."}
            action={
              <Button asChild variant="secondary" size="auto">
                <Link href="/events">All events</Link>
              </Button>
            }
          />
        </Card>
      ) : (
        <div className="flex min-h-tap-xl items-center justify-center">
          <Spinner label="Opening the event" />
        </div>
      )}
    </div>
  );
}
