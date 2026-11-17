import { errorMessage, useLeaderboardProfile, useSetLeaderboardVisibility, type LeaderboardVisibility } from "@scaffoldapp/shared";
import { ErrorState, ListGroup, ListRow, Screen, Skeleton, toast, useTheme } from "@scaffoldapp/ui-native";
import { Check } from "lucide-react-native";

/** Settings → Leaderboard: appear under your alias, your name and photo, or not at all. */
export function LeaderboardSettingsScreen() {
  const theme = useTheme();
  const profile = useLeaderboardProfile();
  const set = useSetLeaderboardVisibility();
  const choose = (v: LeaderboardVisibility) =>
    set.mutate(v, { onSuccess: () => toast.success(v === "hidden" ? "You're off the leaderboards" : "Saved"), onError: (e) => toast.error(errorMessage(e, "Couldn't save. Try again.")) });
  return (
    <Screen edges={["bottom"]} gap={theme.space[6]}>
      {profile.isPending ? (
        <Skeleton height={theme.tap.base * 3} />
      ) : profile.isError ? (
        <ErrorState title="Couldn't load your leaderboard settings" info={profile.error.message} onRetry={() => void profile.refetch()} />
      ) : (
        <ListGroup title="How you appear" info="Changes show on the boards within a minute.">
          {(
            [
              ["alias", `As ${profile.data.alias}`],
              ["name", profile.data.display_name ? `As ${profile.data.display_name}` : "With your name"],
              ["hidden", "Hide me"],
            ] as [LeaderboardVisibility, string][]
          ).map(([v, label]) => (
            <ListRow
              key={v}
              title={label}
              chevron={false}
              trailing={profile.data.visibility === v ? <Check color={theme.colors.primaryStrong} size={20} /> : undefined}
              onPress={() => profile.data.visibility !== v && choose(v)}
            />
          ))}
        </ListGroup>
      )}
    </Screen>
  );
}
