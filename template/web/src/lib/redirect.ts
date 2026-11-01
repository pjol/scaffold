/** Only same-app relative paths are allowed as post-sign-in destinations (no open redirects). */
export function safeRedirect(target: string | null | undefined, fallback = "/"): string {
  if (!target) return fallback;
  try {
    const decoded = decodeURIComponent(target);
    if (!decoded.startsWith("/") || decoded.startsWith("//") || decoded.startsWith("/\\")) return fallback;
    if (decoded.startsWith("/sign-in")) return fallback;
    return decoded;
  } catch {
    return fallback;
  }
}

/** `/sign-in?redirect_url=<path>`. */
export function signInHref(returnTo?: string | null): string {
  return returnTo && returnTo !== "/" ? `/sign-in?redirect_url=${encodeURIComponent(returnTo)}` : "/sign-in";
}
