import {
  basketsIsCsv,
  basketsReplaceLine,
  errorMessage,
  formatBasketsWeight,
  useBasket,
  useBasketsPreview,
  useCreateBasket,
  useDebouncedValue,
  type BasketsPreviewLine,
} from "@scaffoldapp/shared";
import { Banner, Button, Chip, EmptyState, ErrorState, Field, Input, ListGroup, ListRow, Screen, Skeleton, Text, TextArea, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams } from "expo-router";
import { ListPlus } from "lucide-react-native";
import { useEffect, useMemo, useRef, useState } from "react";
import { View } from "react-native";
import { Stat, StatGrid, useExchangeMoney } from "../exchange/parts";
import { BasketLines, PriceStatusBadge } from "./parts";

const LIST_INFO = "One market per line: “2 Gold”, “Silver x3” or “BTC-USD, 1”. A CSV with a header row works too. The quantity defaults to 1.";

function lineTitle(l: BasketsPreviewLine): string {
  return `${l.quantity} × ${l.market ? l.market.name : `“${l.text}”`}`;
}

/** New basket: paste a list, see it resolve and price live, and list it (or join the same one). */
export function NewBasketScreen() {
  const theme = useTheme();
  const s = theme.space;
  const { from } = useLocalSearchParams<{ from?: string }>();
  const { money } = useExchangeMoney();
  const [text, setText] = useState("");
  const [name, setName] = useState("");
  const create = useCreateBasket();

  // ?from=SYMBOL starts from another basket's lines.
  const source = useBasket(from ?? null);
  const prefilled = useRef(false);
  useEffect(() => {
    if (prefilled.current || !source.data) return;
    prefilled.current = true;
    setText(source.data.lines.map((l) => `${l.quantity} ${l.symbol}`).join("\n"));
  }, [source.data]);

  const input = useMemo(() => ({ text }), [text]);
  const debounced = useDebouncedValue(input, 500);
  const preview = useBasketsPreview(debounced);
  const empty = !text.trim();
  const fresh = debounced === input && !preview.isPlaceholderData && !preview.isFetching;
  const p = empty ? undefined : preview.data;
  const ex = p?.existing ?? null;
  const csv = basketsIsCsv(text);
  const joinedAlready = !!ex && ex.entered && ex.status !== "archived";

  const submit = async () => {
    try {
      const res = await create.mutateAsync({ text, name: name.trim() || undefined });
      toast.success(res.created ? "Basket listed" : res.revived ? "Basket reopened" : res.joined ? "You're in this basket" : "You were in this basket already");
      router.replace({ pathname: "/baskets/[symbol]", params: { symbol: res.basket.symbol } });
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't create the basket. Try again."));
    }
  };
  const label = !ex ? "Create basket" : ex.status === "archived" ? `Reopen ${ex.symbol}` : ex.entered ? `Open ${ex.symbol}` : `Join ${ex.symbol}`;

  return (
    <Screen
      edges={["bottom"]}
      keyboard
      gap={s[6]}
      footer={
        <Button
          fullWidth
          loading={create.isPending}
          disabled={!joinedAlready && (!p || !p.creatable || !fresh)}
          onPress={() => {
            if (joinedAlready && ex) router.replace({ pathname: "/baskets/[symbol]", params: { symbol: ex.symbol } });
            else void submit();
          }}
        >
          {label}
        </Button>
      }
    >
      <Stack.Screen options={{ title: "New basket" }} />
      <Field label="Paste a list" info={LIST_INFO}>
        <TextArea value={text} onChangeText={setText} placeholder={"1 Gold\nSilver x40"} autoCorrect={false} autoCapitalize="none" style={{ minHeight: s[32] }} />
      </Field>

      {empty ? (
        <EmptyState compact icon={ListPlus} title="Add markets to see the basket" />
      ) : !p && preview.isError ? (
        <ErrorState compact title="Couldn't check this list" info={preview.error.message} onRetry={() => void preview.refetch()} retrying={preview.isFetching} />
      ) : !p ? (
        <View style={{ gap: s[2] }} accessibilityLabel="Checking the list" accessibilityRole="progressbar">
          <Skeleton height={theme.tap.base} />
          <Skeleton height={theme.tap.base} />
          <Skeleton height={s[24]} />
        </View>
      ) : (
        <View style={{ gap: s[6] }}>
          {ex && !ex.hidden && (
            <Banner
              tone="info"
              title={
                ex.status === "archived"
                  ? `These lines make ${ex.name} (${ex.symbol}), now archived. Creating it reopens it.`
                  : ex.entered
                    ? `These lines make ${ex.name} (${ex.symbol}). You're in it already.`
                    : `These lines make ${ex.name} (${ex.symbol}), listed already. Creating it adds you to it.`
              }
            />
          )}
          {p.blockers.map((b) => (
            <Banner key={b} tone="danger" title={b} />
          ))}
          {p.warnings.map((w) => (
            <Banner key={w} tone="warning" title={w} />
          ))}
          <ListGroup title="Lines">
            {p.lines.map((l, i) => (
              <ListRow
                key={`${l.line}-${i}`}
                title={lineTitle(l)}
                info={l.market ? `${l.market.symbol}${l.message ? ` · ${l.message}` : ""}` : l.message}
                trailing={
                  l.price ? (
                    <Text role="label" tabular>
                      {money(l.price)}
                    </Text>
                  ) : (
                    <PriceStatusBadge status={l.status === "unresolved" ? undefined : l.price_status} />
                  )
                }
              />
            ))}
          </ListGroup>
          {!csv &&
            p.lines
              .filter((l) => l.status === "unresolved" && l.line > 0 && l.candidates.length > 0)
              .map((l) => (
                <View key={`fix-${l.line}`} style={{ gap: s[2] }}>
                  <Text role="label" tone="muted">
                    Line {l.line}: “{l.text}”
                  </Text>
                  <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
                    {l.candidates.map((c) => (
                      <Chip key={c.symbol} size="sm" onPress={() => setText((t) => basketsReplaceLine(t, l.line, `${l.quantity} ${c.symbol}`))}>
                        {c.name}
                      </Chip>
                    ))}
                  </View>
                </View>
              ))}
          {p.basket.length > 0 && (
            <View style={{ gap: s[3] }}>
              <Text role="label" tone="muted" accessibilityRole="header">
                One basket unit
              </Text>
              <BasketLines lines={p.basket} money={money} />
              <StatGrid>
                {[
                  <Stat key="value" label="Value">
                    {money(p.value)}
                  </Stat>,
                  <Stat key="share" label="Largest line">
                    {formatBasketsWeight(p.max_share)}
                  </Stat>,
                  <Stat key="symbol" label="Symbol">
                    {p.symbol ?? "–"}
                  </Stat>,
                  <Stat key="lev" label="Max leverage">
                    {p.spec ? `${p.spec.max_leverage}×` : "–"}
                  </Stat>,
                ]}
              </StatGrid>
            </View>
          )}
        </View>
      )}

      {!ex && (
        <Field label="Name" info="3–48 characters. Left empty, it's named after its largest lines.">
          <Input value={name} onChangeText={setName} maxLength={48} placeholder="Named after its largest lines" />
        </Field>
      )}
    </Screen>
  );
}
