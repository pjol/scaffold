import type { Metadata } from "next";
import { Suspense } from "react";
import { FilesConnectedAccountScreen } from "@/features/files/ConnectedScreens";

export const metadata: Metadata = { title: "Connected drive" };

export default async function Page({ params }: { params: Promise<{ connectionID: string }> }) {
  const { connectionID } = await params;
  return (
    <Suspense>
      <FilesConnectedAccountScreen connId={decodeURIComponent(connectionID)} />
    </Suspense>
  );
}
