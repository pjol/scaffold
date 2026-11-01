import { useLocalSearchParams } from "expo-router";
import { CrmListScreen } from "@/features/crm/ListScreen";

export default function Route() {
  const { owner } = useLocalSearchParams<{ owner?: string }>();
  return <CrmListScreen type="company" mine={owner === "me"} />;
}
