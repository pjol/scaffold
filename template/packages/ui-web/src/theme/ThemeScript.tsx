import { themeInitScript } from "./script";

/**
 * Blocking `<script>` that restores the stored palette and colour mode before first
 * paint. Server-component safe. Render it first in `<head>` and put
 * `suppressHydrationWarning` on `<html>` (the script changes its attributes).
 */
export function ThemeScript({ nonce }: { nonce?: string }) {
  return <script nonce={nonce} dangerouslySetInnerHTML={{ __html: themeInitScript() }} />;
}
