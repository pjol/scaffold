import { Stack, useLocalSearchParams } from "expo-router";
import { PostScreen } from "@/features/forum/PostScreen";

/** /forum/posts/<id>: the path notifications link to (?comment= is ignored on mobile). */
export default function Route() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return (
    <>
      <Stack.Screen options={{ title: "Post" }} />
      <PostScreen id={id} />
    </>
  );
}
