import type { NextConfig } from "next";

/** Remote image sources `next/image` may optimise: the public bucket (S3_PUBLIC_URL) and Clerk profile photos. */
function remotePatterns(): URL[] {
  const patterns = [new URL("https://img.clerk.com/**"), new URL("https://images.clerk.dev/**")];
  const bucket = process.env.NEXT_PUBLIC_S3_PUBLIC_URL;
  if (bucket) {
    try {
      patterns.push(new URL(`${bucket.replace(/\/+$/, "")}/**`));
    } catch {
      // A malformed URL leaves bucket images unoptimised (src/lib/images.ts falls back).
    }
  }
  return patterns;
}

const nextConfig: NextConfig = {
  reactStrictMode: true,
  // Workspace packages ship TypeScript source.
  transpilePackages: ["@scaffoldapp/shared", "@scaffoldapp/tokens", "@scaffoldapp/ui-web"],
  devIndicators: false,
  poweredByHeader: false,
  // AGENTS.md in this folder carries Next's agent rules; keep `next dev` from rewriting it.
  agentRules: false,
  images: {
    remotePatterns: remotePatterns(),
    qualities: [75],
  },
};

export default nextConfig;
