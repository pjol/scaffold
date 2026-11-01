"use client";

import { tokensStylesheet } from "@scaffoldapp/tokens";
import { useEffect } from "react";
import { APP_NAME } from "@/lib/app";

/**
 * Last-resort boundary (the root layout itself failed): no providers, fonts or Tailwind,
 * so plain markup styled with the token variables (light/dark follow the OS).
 */
export default function GlobalError({ error, retry }: { error: Error & { digest?: string }; retry: () => void }) {
  useEffect(() => {
    console.error(error);
  }, [error]);
  return (
    <html lang="en">
      <head>
        <title>{`Something went wrong · ${APP_NAME}`}</title>
        <style dangerouslySetInnerHTML={{ __html: tokensStylesheet() }} />
      </head>
      <body style={{ margin: 0, minHeight: "100dvh", display: "grid", placeItems: "center", background: "var(--app-canvas)", color: "var(--app-text)", fontFamily: "var(--app-font-body)" }}>
        <main role="alert" style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 24, padding: 24, textAlign: "center" }}>
          <h1 style={{ margin: 0, fontSize: "var(--app-type-title-size)", lineHeight: "var(--app-type-title-line)", fontWeight: 600 }}>Something went wrong</h1>
          <button
            type="button"
            onClick={retry}
            style={{ minHeight: 48, padding: "0 20px", border: 0, borderRadius: "var(--app-radius-md)", background: "var(--app-primary)", color: "var(--app-primary-fg)", fontSize: 16, fontWeight: 600, cursor: "pointer" }}
          >
            Retry
          </button>
        </main>
      </body>
    </html>
  );
}
