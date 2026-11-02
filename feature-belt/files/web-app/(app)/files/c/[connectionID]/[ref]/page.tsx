import type { Metadata } from "next";
import { Suspense } from "react";
import { FilesConnectedItemScreen } from "@/features/files/ConnectedScreens";

export const metadata: Metadata = { title: "Connected drive" };

export default async function Page({ params }: { params: Promise<{ connectionID: string; ref: string }> }) {
  const { connectionID, ref } = await params;
  return (
    <Suspense>
      <FilesConnectedItemScreen connId={decodeURIComponent(connectionID)} itemRef={decodeURIComponent(ref)} key={`${connectionID}/${ref}`} />
    </Suspense>
  );
}
