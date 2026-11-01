import Link from "next/link";
import { cn } from "@scaffoldapp/ui-web";
import { APP_NAME } from "@/lib/app";

/**
 * The app's name as a wordmark, linking home. A placeholder until the design round
 * produces the real logo; keep the link and label when replacing it.
 */
export function AppMark({ className, href = "/" }: { className?: string; href?: string }) {
  return (
    <Link href={href} className={cn("flex min-h-control min-w-0 items-center rounded-sm type-title-sm text-fg", className)}>
      <span className="truncate">{APP_NAME}</span>
    </Link>
  );
}
