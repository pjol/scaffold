"use client";

import { cn } from "@scaffoldapp/ui-web";
import { useState } from "react";

/**
 * A connector's logo (the staff-set https image) or its initial on a quiet tile. Square
 * with a small radius: services are not people, so no round avatar.
 */
export function ConnectorIcon({ name, iconUrl, size = "md", className }: { name: string; iconUrl?: string | null; size?: "sm" | "md" | "lg"; className?: string }) {
  const [failed, setFailed] = useState<string | null>(null);
  const box = size === "sm" ? "size-6 rounded-xs type-caption" : size === "lg" ? "size-12 rounded-md type-title-sm" : "size-10 rounded-sm type-label";
  const show = !!iconUrl && failed !== iconUrl;
  return (
    <span aria-hidden className={cn("flex shrink-0 items-center justify-center overflow-hidden border border-border bg-sunken font-semibold text-fg-muted", box, className)}>
      {show ? (
        // Staff-chosen https images from any host: a plain <img> (next/image only optimises known hosts).
        <img src={iconUrl} alt="" className="size-full object-contain p-1" loading="lazy" referrerPolicy="no-referrer" onError={() => setFailed(iconUrl)} />
      ) : (
        (name.trim()[0] ?? "?").toUpperCase()
      )}
    </span>
  );
}
