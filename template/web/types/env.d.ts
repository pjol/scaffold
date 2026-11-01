// Every env var the web app reads, declared so `process.env.X` type-checks under
// noPropertyAccessFromIndexSignature. Next inlines NEXT_PUBLIC_* literals at build
// time. Add a variable here, to scaffold.json (tier "web") and to src/lib/env.ts together.
declare namespace NodeJS {
  interface ProcessEnv {
    readonly NEXT_PUBLIC_API_URL?: string;
    readonly NEXT_PUBLIC_APP_ENV?: string;
    readonly NEXT_PUBLIC_AUTH_DEV?: string;
    readonly NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY?: string;
    readonly NEXT_PUBLIC_PALETTE?: string;
    readonly NEXT_PUBLIC_S3_PUBLIC_URL?: string;
  }
}
