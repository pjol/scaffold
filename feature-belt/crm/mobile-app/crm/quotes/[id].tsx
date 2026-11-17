import { useLocalSearchParams } from "expo-router";
import { CrmQuoteScreen } from "@/features/crm/QuoteScreen";

export default function Route() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return <CrmQuoteScreen id={id} />;
}
