import type { Metadata } from "next";
import { Suspense } from "react";
import { FilesItemScreen } from "@/features/files/FilesScreens";

export const metadata: Metadata = { title: "Files" };

export default async function Page({ params }: { params: Promise<{ itemID: string }> }) {
  const { itemID } = await params;
  return (
    <Suspense>
      <FilesItemScreen itemId={itemID} />
    </Suspense>
  );
}
