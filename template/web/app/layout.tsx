import { activePaletteId, DEFAULT_PALETTE_ID, getPalette } from "@scaffoldapp/tokens";
import { ThemeScript } from "@scaffoldapp/ui-web";
import type { Metadata, Viewport } from "next";
import { Inter } from "next/font/google";
import type { ReactNode } from "react";
import { APP_NAME } from "@/lib/app";
import { Providers } from "@/providers";
import "./globals.css";

/** Body face of the base palette, exposed as --app-face-body (globals.css aliases display to it). */
const inter = Inter({ subsets: ["latin"], display: "swap", variable: "--app-face-body" });

const paletteId = activePaletteId();
const palette = getPalette(paletteId);

export const metadata: Metadata = {
  title: { default: APP_NAME, template: `%s · ${APP_NAME}` },
  applicationName: APP_NAME,
  icons: { icon: [{ url: "/mark.svg", type: "image/svg+xml" }] },
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
  // Phone keyboards shrink the layout viewport, so full-height screens (chat) keep their composer visible.
  interactiveWidget: "resizes-content",
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: palette.colors.light.canvas },
    { media: "(prefers-color-scheme: dark)", color: palette.colors.dark.canvas },
  ],
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    // ThemeScript sets data-palette / data-theme from localStorage before paint, hence suppressHydrationWarning.
    <html lang="en" className={inter.variable} data-palette={paletteId !== DEFAULT_PALETTE_ID ? paletteId : undefined} suppressHydrationWarning>
      <head>
        <ThemeScript />
      </head>
      <body className="min-h-dvh">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
