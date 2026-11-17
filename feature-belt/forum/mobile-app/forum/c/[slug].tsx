import { useLocalSearchParams } from "expo-router";
import { CommunityScreen } from "@/features/forum/CommunityScreen";

/** /forum/c/<slug>: one community. */
export default function Route() {
  const { slug } = useLocalSearchParams<{ slug: string }>();
  return <CommunityScreen slug={slug} />;
}
