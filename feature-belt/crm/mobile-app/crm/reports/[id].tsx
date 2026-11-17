import { useLocalSearchParams } from "expo-router";
import { CrmReportDashboardScreen } from "@/features/crm/ReportsScreen";

export default function Route() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return <CrmReportDashboardScreen id={id} />;
}
