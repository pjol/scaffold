import { Suspense } from "react";
import { ThreadScreen } from "@/features/chat";

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense>
      <ThreadScreen id={id} />
    </Suspense>
  );
}
