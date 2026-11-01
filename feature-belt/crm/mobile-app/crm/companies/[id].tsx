import { useLocalSearchParams } from "expo-router";
import { CrmRecordScreen } from "@/features/crm/RecordScreen";

export default function Route() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return <CrmRecordScreen type="company" id={id} />;
}
