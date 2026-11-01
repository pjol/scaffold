import { Suspense } from "react";
import { PageScreen } from "@/features/docs";

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense>
      <PageScreen id={id} />
    </Suspense>
  );
}
