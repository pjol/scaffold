import { InfoTip, cn } from "@scaffoldapp/ui-web";
import type { ReactNode } from "react";

/** Screen title (+ⓘ) with an optional action on the right. No subtitle. */
export function PageHeader({ title, info, action, className }: { title: ReactNode; info?: ReactNode; action?: ReactNode; className?: string }) {
  return (
    <div className={cn("flex min-h-control-lg items-center gap-3", className)}>
      <h1 className="flex min-w-0 flex-1 items-center gap-2 type-title text-fg">
        <span className="truncate">{title}</span>
        {info && <InfoTip content={info} size="md" />}
      </h1>
      {action && <div className="flex shrink-0 items-center gap-2">{action}</div>}
    </div>
  );
}

/** A titled group inside a screen: heading and content in one 12 px stack. */
export function Section({ title, info, action, children, className }: { title: ReactNode; info?: ReactNode; action?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cn("flex flex-col gap-3", className)}>
      <div className="flex min-h-control items-center gap-3">
        <h2 className="flex min-w-0 flex-1 items-center gap-2 type-title-sm text-fg">
          {title}
          {info && <InfoTip content={info} />}
        </h2>
        {action}
      </div>
      {children}
    </section>
  );
}

/**
 * A screen: title (+ⓘ, action), then sections. Title → body is 24, so a first section
 * heading sits 2× further from the title than from its own content (12); sections are
 * `gap-section` apart.
 */
export function Screen({ title, info, action, children, className }: { title: ReactNode; info?: ReactNode; action?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <div className={cn("flex flex-col gap-6", className)}>
      <PageHeader title={title} {...(info ? { info } : {})} {...(action ? { action } : {})} />
      <div className="flex flex-col gap-section">{children}</div>
    </div>
  );
}
