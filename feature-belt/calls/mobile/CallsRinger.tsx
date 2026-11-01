import { useCallsRings } from "@scaffoldapp/shared";
import { router, usePathname } from "expo-router";
import { useEffect, useRef } from "react";

/**
 * Session mount: when a call starts ringing for you, opens the incoming-call screen
 * (unless you're already on that meeting). The screen closes itself when the ring ends.
 * Rings arrive over realtime while the app is open; a backgrounded app learns about a
 * missed call from its push notification (see FEATURE.md → Extending for VoIP push).
 */
export function CallsRinger() {
  const rings = useCallsRings();
  const pathname = usePathname();
  const shown = useRef<string | null>(null);
  const ring = rings.data?.items.find((r) => r.status === "ringing" && Date.parse(r.expires_at) > Date.now());

  useEffect(() => {
    if (!ring || shown.current === ring.id) return;
    if (pathname === `/meet/${ring.code}` || pathname.startsWith("/calls/incoming")) return;
    shown.current = ring.id;
    router.push({ pathname: "/calls/incoming", params: { ring: ring.id } });
  }, [ring, pathname]);
  return null;
}
