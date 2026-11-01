import { CRM_PATH, crmMoney, hasErrorCode, orgsNounA, orgsNounTitle, useActiveOrg, useCrmSettings, type CrmObjectType, type CrmSettings, type CrmStageOutcome } from "@scaffoldapp/shared";
import { Badge, Button, EmptyState, ErrorState, Screen, Skeleton, useTheme, type BadgeTone } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { Building, Building2, Handshake, Lock, UserRound } from "lucide-react-native";
import type { ReactNode } from "react";
import { View } from "react-native";

export const TYPE_ICON: Record<CrmObjectType, typeof Building2> = { company: Building2, contact: UserRound, deal: Handshake };

/** App path of a record (the same on web and mobile, and in notifications). */
export function recordPath(type: CrmObjectType, id: string): `/crm/${"companies" | "contacts" | "deals"}/${string}` {
  return `/crm/${CRM_PATH[type]}/${id}`;
}

export const OUTCOME_TONE: Record<CrmStageOutcome, BadgeTone> = { open: "neutral", won: "success", lost: "danger" };

export function money(minor: number | null | undefined, currency: string): string {
  return minor === null || minor === undefined ? "No amount" : crmMoney(minor, currency);
}

/** "Today", "Tomorrow", "Mon, Nov 3" for a YYYY-MM-DD or ISO date. */
export function dayLabel(value: string, now = new Date()): string {
  const d = value.length === 10 ? new Date(`${value}T12:00:00`) : new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  const start = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((start(d) - start(now)) / 86_400_000);
  if (diff === 0) return "Today";
  if (diff === 1) return "Tomorrow";
  if (diff === -1) return "Yesterday";
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric", ...(d.getFullYear() !== now.getFullYear() ? { year: "numeric" } : {}) });
}

export function timeLabel(iso: string): string {
  return new Date(iso).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
}

export function StageBadge({ name, outcome }: { name: string; outcome: CrmStageOutcome }) {
  return (
    <Badge tone={OUTCOME_TONE[outcome]} dot={outcome !== "open"}>
      {name}
    </Badge>
  );
}

/**
 * The active org's CRM for a screen: renders the honest state when there is no org, or
 * you don't hold "Use the CRM", and otherwise calls children with the org and settings.
 */
export function CrmGate({ children }: { children: (ctx: { orgId: string; orgName: string; settings: CrmSettings }) => ReactNode }) {
  const theme = useTheme();
  const { org, isPending } = useActiveOrg();
  const settings = useCrmSettings(org?.id);
  if (isPending || (org && settings.isPending)) {
    return (
      <Screen edges={["bottom"]} gap={theme.space[4]}>
        <Skeleton height={theme.tap.lg} />
        <Skeleton height={theme.tap.lg * 3} />
      </Screen>
    );
  }
  if (!org) {
    return (
      <Screen edges={["bottom"]} centered>
        <EmptyState icon={Building} title={`Join or create ${orgsNounA()} first`} action={<Button onPress={() => router.push("/settings/orgs")}>{orgsNounTitle("many")}</Button>} />
      </Screen>
    );
  }
  if (settings.isError) {
    const denied = hasErrorCode(settings.error, "permission_required");
    const message = settings.error.message;
    return (
      <Screen edges={["bottom"]} centered>
        {denied ? (
          <EmptyState icon={Lock} title={`You don't use the CRM in ${org.name}`} info="An admin can give you the “Use the CRM” permission." />
        ) : (
          <ErrorState title="Couldn't load the CRM" info={message} onRetry={() => void settings.refetch()} retrying={settings.isRefetching} />
        )}
      </Screen>
    );
  }
  if (!settings.data) return null;
  return <>{children({ orgId: org.id, orgName: org.name, settings: settings.data })}</>;
}

/** A row of small facts under a title. */
export function FactRow({ children }: { children: ReactNode }) {
  const theme = useTheme();
  return <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", columnGap: theme.space[3], rowGap: theme.space[1] }}>{children}</View>;
}
