"use client";

import { toast } from "@scaffoldapp/ui-web";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";

/**
 * Picture-in-picture: Document PiP where the browser has it (Chromium: a small always-on-top
 * window we render the speaker and controls into), else the browser's video PiP of the
 * featured tile's video.
 */

interface DocumentPiP {
  requestWindow(options?: { width?: number; height?: number }): Promise<Window>;
  window: Window | null;
}

function documentPiP(): DocumentPiP | null {
  if (typeof window === "undefined") return null;
  return (window as unknown as { documentPictureInPicture?: DocumentPiP }).documentPictureInPicture ?? null;
}

export function pipSupported(): boolean {
  if (typeof document === "undefined") return false;
  return !!documentPiP() || ("pictureInPictureEnabled" in document && (document as Document & { pictureInPictureEnabled?: boolean }).pictureInPictureEnabled === true);
}

/** Copies the page's styles and theme into the PiP window so our tokens apply there. */
function adoptStyles(win: Window) {
  for (const sheet of Array.from(document.styleSheets)) {
    try {
      const style = win.document.createElement("style");
      style.textContent = Array.from(sheet.cssRules)
        .map((r) => r.cssText)
        .join("\n");
      win.document.head.appendChild(style);
    } catch {
      if (sheet.href) {
        const link = win.document.createElement("link");
        link.rel = "stylesheet";
        link.href = sheet.href;
        win.document.head.appendChild(link);
      }
    }
  }
  const html = document.documentElement;
  win.document.documentElement.className = html.className;
  for (const a of Array.from(html.attributes)) if (a.name.startsWith("data-") || a.name === "style") win.document.documentElement.setAttribute(a.name, a.value);
  win.document.body.className = "bg-canvas text-fg";
}

export function usePictureInPicture() {
  const [win, setWin] = useState<Window | null>(null);
  const close = useCallback(() => {
    win?.close();
    setWin(null);
  }, [win]);
  useEffect(() => {
    if (!win) return undefined;
    const done = () => setWin(null);
    win.addEventListener("pagehide", done);
    return () => win.removeEventListener("pagehide", done);
  }, [win]);
  useEffect(() => () => win?.close(), [win]);

  const open = useCallback(async () => {
    const d = documentPiP();
    try {
      if (d) {
        const w = await d.requestWindow({ width: 400, height: 260 });
        adoptStyles(w);
        setWin(w);
        return;
      }
      const video = document.querySelector<HTMLVideoElement>("[data-calls-featured] video") ?? document.querySelector<HTMLVideoElement>("[data-calls-stage] video");
      if (!video) {
        toast("Nobody's video is on to show in a small window.");
        return;
      }
      if (document.pictureInPictureElement) await document.exitPictureInPicture();
      else await video.requestPictureInPicture();
    } catch {
      toast.error("Your browser didn't open the small window.");
    }
  }, []);

  return { win, open, close, active: !!win };
}

/** Renders children into the PiP window. */
export function PipPortal({ win, children }: { win: Window | null; children: ReactNode }) {
  if (!win) return null;
  return createPortal(children, win.document.body);
}
