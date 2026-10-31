import { errorMessage, useJoinEvent } from "@scaffoldapp/shared";
import { Button, EmptyState, Screen, Spinner } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { Unlink } from "lucide-react-native";
import { useEffect, useRef } from "react";

/** events/join?code=…: joins the invite list with an invite link code, then opens the event. */
export function JoinEventScreen({ code }: { code: string }) {
  const join = useJoinEvent();
  const started = useRef(false);

  useEffect(() => {
    if (!code || started.current) return;
    started.current = true;
    join.mutate(code, { onSuccess: (event) => router.replace({ pathname: "/events/[id]", params: { id: event.id } }) });
  }, [code, join]);

  if (!code || join.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        <EmptyState
          icon={Unlink}
          title="That invite link doesn't work anymore"
          info={join.isError ? errorMessage(join.error, "Ask the host for a new link.") : "Ask the host for a new link."}
          action={
            <Button variant="secondary" onPress={() => router.replace("/events")}>
              All events
            </Button>
          }
        />
      </Screen>
    );
  }
  return (
    <Screen edges={["bottom"]} centered>
      <Spinner label="Opening the event" />
    </Screen>
  );
}
