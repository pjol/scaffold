import { cn } from "@scaffoldapp/ui-web";
import Link from "next/link";

const linkClass = "flex min-h-control items-center rounded-sm px-2 transition-colors hover:text-fg pointer-coarse:min-h-tap";

/** Terms · Privacy · Support, for footers and the sign-in and consent screens. */
export function LegalLinks({ className, support = true }: { className?: string; support?: boolean }) {
  return (
    <nav aria-label="Legal" className={cn("flex flex-wrap items-center gap-x-2 type-label text-fg-muted", className)}>
      <Link href="/legal/terms" className={linkClass}>
        Terms of Service
      </Link>
      <Link href="/legal/privacy" className={linkClass}>
        Privacy Policy
      </Link>
      {support && (
        <Link href="/support" className={linkClass}>
          Support
        </Link>
      )}
    </nav>
  );
}
