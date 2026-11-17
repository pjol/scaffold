import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmTicketBoardScreen } from "@/features/crm/TicketBoardScreen";

export const metadata: Metadata = { title: "Tickets board" };

export default function Page() {
  return (
    <Suspense>
      <CrmTicketBoardScreen />
    </Suspense>
  );
}
