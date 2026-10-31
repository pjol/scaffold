import { S3_PUBLIC_URL } from "./env";

const OPTIMISABLE = ["https://img.clerk.com/", "https://images.clerk.dev/", ...(S3_PUBLIC_URL ? [`${S3_PUBLIC_URL}/`] : [])];

/**
 * Whether `next/image` may optimise `src` (it matches `images.remotePatterns` in
 * next.config.ts). Anything else, including development uploads served by the API, is
 * rendered with `unoptimized` so an unexpected host never breaks a page.
 */
export function isOptimisable(src: string): boolean {
  return OPTIMISABLE.some((prefix) => src.startsWith(prefix));
}
