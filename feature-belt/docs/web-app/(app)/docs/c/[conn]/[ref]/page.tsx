import { Suspense } from "react";
import { ConnectedPageScreen } from "@/features/docs";

export default async function Page({ params }: { params: Promise<{ conn: string; ref: string }> }) {
  const { conn, ref } = await params;
  return (
    <Suspense>
      <ConnectedPageScreen connectionId={decodeURIComponent(conn)} pageRef={decodeURIComponent(ref)} />
    </Suspense>
  );
}
