import { Skeleton } from "@scaffoldapp/ui-web";

/** The shell's layout in skeleton form while the session and `me` load. */
export function FrameSkeleton() {
  return (
    <div className="min-h-dvh bg-canvas lg:flex" role="status" aria-label="Loading" aria-busy>
      <div className="hidden h-dvh w-64 shrink-0 flex-col border-r border-border bg-surface lg:flex">
        <div className="flex h-topbar items-center px-5">
          <Skeleton className="h-6 w-32" />
        </div>
        <div className="flex flex-col gap-1 px-2 py-4">
          <Skeleton className="h-10" />
          <Skeleton className="h-10" />
          <Skeleton className="h-10" />
        </div>
      </div>
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex h-topbar items-center justify-between gap-3 px-gutter">
          <Skeleton className="h-6 w-32 lg:invisible" />
          <Skeleton shape="circle" className="size-8" />
        </div>
        <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
          <Skeleton className="h-8 w-48" />
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <Skeleton className="h-24" />
            <Skeleton className="h-24" />
            <Skeleton className="h-24" />
          </div>
        </div>
      </div>
    </div>
  );
}
