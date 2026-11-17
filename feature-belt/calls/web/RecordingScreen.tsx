"use client";

import { useCallsRecording } from "@scaffoldapp/shared";
import { Button } from "@scaffoldapp/ui-web";
import { ArrowLeft } from "lucide-react";
import { useRouter } from "next/navigation";
import { Screen } from "@/components/shell/PageHeader";
import { RecordingPlayer } from "./Records";

/** `/calls/recordings/[id]`: one recording, played in the page. */
export function RecordingScreen({ id }: { id: string }) {
  const router = useRouter();
  const rec = useCallsRecording(id);
  const back = rec.data?.session_id ? `/calls/${rec.data.session_id}` : rec.data ? `/calls/meetings/${rec.data.meeting_id}` : "/calls";
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-4 px-gutter py-section">
      <Button variant="ghost" size="sm" leftIcon={<ArrowLeft />} className="self-start" onClick={() => router.push(back)}>
        {rec.data?.session_id ? "Call" : "Calls"}
      </Button>
      <Screen title={rec.data?.meeting_title || "Recording"}>
        <RecordingPlayer id={id} />
      </Screen>
    </div>
  );
}
