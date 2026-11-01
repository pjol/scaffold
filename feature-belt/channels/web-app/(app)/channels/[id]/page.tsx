import { Suspense } from "react";
import { ChannelScreen } from "@/features/channels";

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense>
      <ChannelScreen id={id} />
    </Suspense>
  );
}
