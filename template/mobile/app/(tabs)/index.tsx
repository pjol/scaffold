import { useMe } from "@scaffoldapp/shared";
import { Banner, Button, EmptyState, ListGroup, ListRow, Badge, Screen, Skeleton, Title, enterUp, useTheme } from "@scaffoldapp/ui-native";
import { router, useScrollToTop } from "expo-router";
import { LayoutGrid } from "lucide-react-native";
import { useRef } from "react";
import { View, type ScrollView } from "react-native";
import Animated from "react-native-reanimated";
import { homeEntries, type HomeEntry } from "@/features/registry";

function EntryRow({ entry }: { entry: HomeEntry }) {
  const theme = useTheme();
  const badge = entry.useBadge?.();
  const Icon = entry.icon;
  return (
    <ListRow
      title={entry.title}
      {...(entry.info ? { info: entry.info } : {})}
      leading={<Icon size={22} color={theme.colors.textMuted} strokeWidth={2} />}
      trailing={badge !== undefined && badge !== 0 ? <Badge size="sm" tone="danger" variant="solid">{String(badge)}</Badge> : undefined}
      onPress={() => router.push(entry.href)}
    />
  );
}

/** Home: a greeting and the installed features. Replace with the product's own home. */
export default function HomeScreen() {
  const theme = useTheme();
  const s = theme.space;
  const ref = useRef<ScrollView>(null);
  useScrollToTop(ref);
  const me = useMe();
  const firstName = me.data?.display_name.trim().split(/\s+/)[0];

  return (
    <Screen ref={ref} edges={["top"]}>
      <View style={{ gap: s[4] }}>
        {me.isPending ? <Skeleton width="60%" height={theme.type.title.lineHeight} /> : <Title>{firstName ? `Hi, ${firstName}` : "Home"}</Title>}
        {me.data?.status === "suspended" ? <Banner tone="warning" title="Your account is suspended. You can read but not post." action={{ label: "Support", onPress: () => router.push("/support") }} /> : null}
      </View>
      {homeEntries.length > 0 ? (
        <Animated.View entering={enterUp(theme, 1)}>
          <ListGroup>
            {homeEntries.map((entry) => (
              <EntryRow key={entry.id} entry={entry} />
            ))}
          </ListGroup>
        </Animated.View>
      ) : (
        <EmptyState
          icon={LayoutGrid}
          title="Nothing here yet"
          action={
            <Button variant="secondary" onPress={() => router.push("/settings")}>
              Open settings
            </Button>
          }
        />
      )}
    </Screen>
  );
}
