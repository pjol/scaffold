import { catalogAttrLabel, errorMessage, formatCatalogPrice, useCatalogItem } from "@scaffoldapp/shared";
import { Badge, EmptyState, ErrorState, Headline, InfoTip, Label, ListGroup, ListRow, Screen, Skeleton, Text, Title, useTheme } from "@scaffoldapp/ui-native";
import { Stack, useLocalSearchParams } from "expo-router";
import { ScanSearch } from "lucide-react-native";
import { RefreshControl, View } from "react-native";
import { CatalogThumb } from "./CatalogScreen";

function day(iso: string): string {
  return new Date(iso).toLocaleDateString("en-US", { year: "numeric", month: "short", day: "numeric" });
}

/** One item: its other names, attributes and reference price. */
export function CatalogItemScreen() {
  const theme = useTheme();
  const { key } = useLocalSearchParams<{ key: string }>();
  const item = useCatalogItem(key || null);

  if (item.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "" }} />
        <Skeleton height={32} width="60%" />
        <Skeleton height={120} />
        <Skeleton height={120} />
      </Screen>
    );
  }
  if (item.isError) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "" }} />
        {item.error.status === 404 ? (
          <EmptyState icon={ScanSearch} title="No such item" />
        ) : (
          <ErrorState title="Couldn't load this item" info={errorMessage(item.error)} onRetry={() => void item.refetch()} retrying={item.isRefetching} />
        )}
      </Screen>
    );
  }

  const it = item.data;
  const price = formatCatalogPrice(it.ref_price, it.currency);
  const attrs = Object.entries(it.attrs).sort(([a], [b]) => a.localeCompare(b));

  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={item.isRefetching} onRefresh={() => void item.refetch()} />}>
      <Stack.Screen options={{ title: it.name }} />
      <View style={{ flexDirection: "row", gap: theme.space[4], alignItems: "flex-start" }}>
        {it.image_url ? <CatalogThumb item={it} size={96} /> : null}
        <View style={{ flex: 1, gap: theme.space[2] }}>
          <Title>{it.name}</Title>
          <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: theme.space[2] }}>
            {it.category ? <Badge size="sm">{it.category}</Badge> : null}
            {it.retired ? (
              <Badge tone="warning" size="sm">
                {it.retired_by === "staff" ? "Retired by staff" : "Retired"}
              </Badge>
            ) : null}
            {it.subtitle ? <Label tone="muted">{it.subtitle}</Label> : null}
          </View>
          {price ? (
            <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
              <Headline tabular>{price}</Headline>
              <InfoTip content="A reference price from the catalog's source, not a live quote." label="About this price" />
            </View>
          ) : null}
        </View>
      </View>

      <ListGroup title="Details">
        <ListRow
          title="Key"
          trailing={
            <Text role="label" tone="muted" selectable>
              {it.key}
            </Text>
          }
        />
        {it.retired && it.retired_at ? (
          <ListRow
            title="Retired"
            info={it.retired_by === "staff" ? "Retired by staff: it stays out of search until staff restore it." : "The source no longer lists it. It comes back if the source lists it again."}
            trailing={<Text role="label">{day(it.retired_at)}</Text>}
          />
        ) : null}
        <ListRow title="First seen" trailing={<Text role="label">{day(it.first_seen_at)}</Text>} />
        <ListRow title="Updated" trailing={<Text role="label">{day(it.updated_at)}</Text>} />
      </ListGroup>

      {it.aliases.length > 0 ? (
        <View style={{ gap: theme.space[2] }}>
          <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[1] }}>
            <Label tone="muted" weight={600}>
              Also known as
            </Label>
            <InfoTip content="Other names this item is found and resolved by." label="About other names" />
          </View>
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: theme.space[2] }}>
            {it.aliases.map((a) => (
              <Badge key={a} size="md">
                {a}
              </Badge>
            ))}
          </View>
        </View>
      ) : null}

      {attrs.length > 0 ? (
        <ListGroup title="Attributes">
          {attrs.map(([k, v]) => (
            <ListRow
              key={k}
              title={catalogAttrLabel(k)}
              trailing={
                <Text role="label" numberOfLines={2} align="right" style={{ flexShrink: 1 }}>
                  {v || "—"}
                </Text>
              }
            />
          ))}
        </ListGroup>
      ) : null}
    </Screen>
  );
}
