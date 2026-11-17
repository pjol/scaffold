import { errorMessage, useSocialPeople, useSocialSuggestions } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, Input, ListGroup, Screen, useTheme } from "@scaffoldapp/ui-native";
import { Search, SearchX, Users } from "lucide-react-native";
import { useState } from "react";
import { RefreshControl, View } from "react-native";
import { PeopleSkeleton, PersonRow } from "./parts";

function Suggestions() {
  const s = useSocialSuggestions();
  return (
    <ListGroup title="Suggested for you" info="People followed by people you follow, then the most followed.">
      {s.isPending ? (
        <PeopleSkeleton />
      ) : s.isError ? (
        <ErrorState compact title="Couldn't load suggestions" info={errorMessage(s.error)} onRetry={() => void s.refetch()} retrying={s.isFetching} />
      ) : s.data.items.length === 0 ? (
        <EmptyState compact icon={Users} title="Nobody to suggest yet" />
      ) : (
        <View>
          {s.data.items.map((p) => (
            <PersonRow key={p.user_id} person={p} info={p.mutuals ? `Followed by ${p.mutuals} ${p.mutuals === 1 ? "person" : "people"} you follow` : undefined} />
          ))}
        </View>
      )}
    </ListGroup>
  );
}

/** People: search by name or @handle; suggestions while the box is empty. */
export function PeopleScreen() {
  const theme = useTheme();
  const [q, setQ] = useState("");
  const people = useSocialPeople(q);
  const suggestions = useSocialSuggestions();
  const typed = q.trim().replace(/^@/, "");

  return (
    <Screen
      edges={["bottom"]}
      gap={theme.space[6]}
      refreshControl={<RefreshControl refreshing={suggestions.isRefetching} onRefresh={() => void (typed ? people.refetch() : suggestions.refetch())} />}
    >
      <Input
        value={q}
        onChangeText={setQ}
        placeholder="Search by name or @handle"
        accessibilityLabel="Search people"
        leadingIcon={Search}
        autoCapitalize="none"
        autoCorrect={false}
        returnKeyType="search"
        clearButtonMode="while-editing"
      />
      {typed.length === 0 ? (
        <Suggestions />
      ) : (
        <ListGroup title="Results">
          {typed.length < 2 ? (
            <EmptyState compact icon={SearchX} title="Type 2 or more characters" />
          ) : people.isPending || people.query !== q.trim() ? (
            <PeopleSkeleton rows={3} />
          ) : people.isError ? (
            <ErrorState compact title="Couldn't search people" info={errorMessage(people.error)} onRetry={() => void people.refetch()} retrying={people.isFetching} />
          ) : people.data.items.length === 0 ? (
            <EmptyState
              compact
              icon={SearchX}
              title={`Nobody matches “${q.trim()}”`}
              action={
                <Button variant="secondary" onPress={() => setQ("")}>
                  Clear search
                </Button>
              }
            />
          ) : (
            <View>
              {people.data.items.map((p) => (
                <PersonRow key={p.user_id} person={p} />
              ))}
            </View>
          )}
        </ListGroup>
      )}
    </Screen>
  );
}
