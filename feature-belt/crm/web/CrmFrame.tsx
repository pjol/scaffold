"use client";

import {
  hasErrorCode,
  orgsNounA,
  orgsNounTitle,
  useActiveOrg,
  useCrmSettings,
  useCrmSources,
  useCrmTaskSummary,
  useOrgPickerVisible,
  type CrmSettings,
  type CrmSources,
} from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { Building2, Building, CheckSquare, Handshake, LayoutDashboard, Lock, Settings2, Upload, UserRound } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { ScrollTabs, type SectionLink } from "@/components/shell/SectionNav";
import { OrgSwitcher } from "@/features/orgs";

export interface CrmContextValue {
  orgId: string;
  orgName: string;
  settings: CrmSettings;
  /** Where records come from (the app, connected HubSpot/Salesforce accounts); undefined while loading. */
  sources: CrmSources | undefined;
}

const Ctx = createContext<CrmContextValue | null>(null);

/** The active org and its CRM settings, inside a CrmFrame. */
export function useCrm(): CrmContextValue {
  const v = useContext(Ctx);
  if (!v) throw new Error("useCrm outside CrmFrame");
  return v;
}

function useTaskBadge(): number | undefined {
  const { org } = useActiveOrg();
  const s = useCrmTaskSummary(org?.id);
  return s.data ? s.data.overdue + s.data.today || undefined : undefined;
}

const LINKS: SectionLink[] = [
  { href: "/crm", label: "Dashboard", icon: LayoutDashboard, active: (p) => p === "/crm" },
  { href: "/crm/contacts", label: "Contacts", icon: UserRound },
  { href: "/crm/companies", label: "Companies", icon: Building2 },
  { href: "/crm/deals", label: "Deals", icon: Handshake },
  { href: "/crm/tasks", label: "Tasks", icon: CheckSquare, useBadge: useTaskBadge },
];

/**
 * Every CRM screen: the org (switcher when you have several), section tabs and the CRM ⋮
 * menu (import, settings), then the screen. Shows the honest state when there is no
 * org or you don't hold "Use the CRM".
 */
export function CrmFrame({ children, actions }: { children: ReactNode; actions?: ReactNode }) {
  const { org, orgs, isPending } = useActiveOrg();
  const pickerVisible = useOrgPickerVisible();
  const settings = useCrmSettings(org?.id);
  // An older backend has no source_mode: the app's own CRM.
  const connectedMode = !!settings.data && (settings.data.source_mode ?? "app") !== "app";
  const sources = useCrmSources(connectedMode ? org?.id : null);
  const pathname = usePathname() ?? "/crm";
  const router = useRouter();

  const denied = settings.isError && hasErrorCode(settings.error, "permission_required");
  let body: ReactNode;
  if (isPending || (org && settings.isPending) || (connectedMode && sources.isPending)) {
    body = (
      <SkeletonGroup label="Loading the CRM" className="flex flex-col gap-4">
        <Skeleton className="h-10 w-full max-w-md" />
        <Skeleton className="h-64 w-full" />
      </SkeletonGroup>
    );
  } else if (!org) {
    body = (
      <Card padding="compact">
        <EmptyState
          icon={<Building />}
          title={`Join or create ${orgsNounA()} first`}
          action={
            <Button asChild size="auto">
              <Link href="/settings/orgs">{orgsNounTitle("many")}</Link>
            </Button>
          }
        />
      </Card>
    );
  } else if (settings.isError) {
    body = denied ? (
      <Card padding="compact">
        <EmptyState icon={<Lock />} title={`You don't use the CRM in ${org.name}`} info="An admin can give you the “Use the CRM” permission in Settings → Permissions." />
      </Card>
    ) : (
      <ErrorState title="Couldn't load the CRM" info={settings.error.message} onRetry={() => void settings.refetch()} retrying={settings.isRefetching} />
    );
  } else if (settings.data) {
    body = <Ctx.Provider value={{ orgId: org.id, orgName: org.name, settings: settings.data, sources: connectedMode ? sources.data : undefined }}>{children}</Ctx.Provider>;
  }

  const can = settings.data?.can;
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
      <header className="flex flex-col gap-4">
        <div className="flex min-h-control-lg flex-wrap items-center gap-3">
          <h1 className="type-title text-fg">CRM</h1>
          {orgs.length > 1 && pickerVisible ? (
            <div className="order-last w-full sm:order-none sm:w-auto">
              <OrgSwitcher />
            </div>
          ) : null}
          <div className="ml-auto flex items-center gap-2">
            {settings.data ? actions : null}
            {settings.data ? (
              <ResourceMenu
                feature="crm"
                orgId={org?.id}
                title="CRM"
                label="CRM actions"
                actions={[
                  ...(can?.import ? [{ id: "crm-import", label: "Import CSV", icon: Upload, onSelect: () => router.push("/crm/import") }] : []),
                  { id: "crm-settings", label: can?.manage_settings ? "CRM settings" : "Pipelines and properties", icon: Settings2, onSelect: () => router.push("/crm/settings") },
                ]}
              />
            ) : null}
          </div>
        </div>
        {settings.data ? <ScrollTabs links={LINKS} pathname={pathname} label="CRM sections" /> : null}
      </header>
      {body}
    </div>
  );
}
