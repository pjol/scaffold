import { Button, EmptyState } from "@scaffoldapp/ui-web";
import { SearchX } from "lucide-react";
import Link from "next/link";
import { CenteredScreen } from "@/components/shell/CenteredScreen";

export default function NotFound() {
  return (
    <CenteredScreen>
      <EmptyState
        icon={<SearchX />}
        title="Page not found"
        action={
          <Button asChild size="auto">
            <Link href="/">Go home</Link>
          </Button>
        }
      />
    </CenteredScreen>
  );
}
