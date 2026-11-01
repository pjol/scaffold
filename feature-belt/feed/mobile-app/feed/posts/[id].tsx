import { Stack, useLocalSearchParams } from "expo-router";
import { PostScreen } from "@/features/feed/PostScreen";

/** /feed/posts/<id>: the path notifications link to. */
export default function Route() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return (
    <>
      <Stack.Screen options={{ title: "Post" }} />
      <PostScreen id={id} />
    </>
  );
}
