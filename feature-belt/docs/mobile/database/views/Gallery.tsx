import { docsTitle, useDocsFileUrl, type DocsFileValue, type DocsRow } from "@scaffoldapp/shared";
import { Text, useTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { useState } from "react";
import { Pressable, View } from "react-native";
import { CoverArt } from "../../PageIcon";
import type { ViewCtx } from "../context";
import { Grouped, RowValues } from "./List";

const IMAGE = /^image\//;
const IMAGE_EXT = /\.(png|jpe?g|gif|webp|heic|heif|avif)$/i;

/** The first uploaded image of a files property, as a card preview (links aren't fetched). */
function FilePreviewImage({ file, token, height, fit }: { file: DocsFileValue; token: string | null; height: number; fit: boolean }) {
  const theme = useTheme();
  const url = useDocsFileUrl(file.file_id ?? null, token);
  const src = url.data?.url;
  return (
    <View style={{ height, backgroundColor: theme.colors.sunken }}>
      {src ? <Image source={{ uri: src }} style={{ width: "100%", height: "100%" }} contentFit={fit ? "contain" : "cover"} accessibilityIgnoresInvertColors /> : null}
    </View>
  );
}

/** The start of the row page's content (the rows answer carries it for gallery views). */
function Snippet({ row, height }: { row: DocsRow; height: number }) {
  const theme = useTheme();
  return (
    <View style={{ height, padding: theme.space[3], backgroundColor: theme.colors.sunken, overflow: "hidden" }}>
      {row.snippet ? (
        <Text role="caption" tone="muted" numberOfLines={Math.max(2, Math.floor((height - theme.space[6]) / 16))}>
          {row.snippet}
        </Text>
      ) : null}
    </View>
  );
}

function Preview({ ctx, row, height }: { ctx: ViewCtx; row: DocsRow; height: number }) {
  const preview = ctx.config.card?.preview ?? "cover";
  const fit = !!ctx.config.card?.fit;
  if (preview === "none") return null;
  if (preview === "content") return <Snippet row={row} height={height} />;
  if (preview === "cover") {
    if (row.cover) return <CoverArt cover={row.cover} token={ctx.token} height={height} />;
    // Without a cover: the first image of any files property.
    const files = ctx.schema.filter((p) => p.type === "files").flatMap((p) => (Array.isArray(row.props[p.id]) ? (row.props[p.id] as DocsFileValue[]) : []));
    const img = files.find((f) => !!f.file_id && (IMAGE.test(f.mime ?? "") || IMAGE_EXT.test(f.name)));
    return img ? <FilePreviewImage file={img} token={ctx.token} height={height} fit={fit} /> : null;
  }
  const list = Array.isArray(row.props[preview]) ? (row.props[preview] as DocsFileValue[]) : [];
  const img = list.find((f) => !!f.file_id && (IMAGE.test(f.mime ?? "") || IMAGE_EXT.test(f.name)));
  return img ? <FilePreviewImage file={img} token={ctx.token} height={height} fit={fit} /> : <View style={{ height }} />;
}

/** Gallery: cards with a cover or image preview, sized small / medium / large. */
export function GalleryView({ ctx }: { ctx: ViewCtx }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [width, setWidth] = useState(0);
  const size = ctx.config.card?.size ?? "medium";
  const cols = size === "large" ? (width >= 560 ? 2 : 1) : size === "small" ? (width >= 360 ? 3 : 2) : width >= 560 ? 3 : 2;
  const gap = s[3];
  const cardW = width ? Math.floor((width - gap * (cols - 1)) / cols) : 0;
  const previewH = size === "large" ? 180 : size === "small" ? 80 : 112;
  return (
    <Grouped
      ctx={ctx}
      render={(rows) => (
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap }} onLayout={(e) => setWidth(Math.floor(e.nativeEvent.layout.width))}>
          {!cardW ? null : rows.map((r) => (
            <Pressable
              key={r.id}
              accessibilityRole="link"
              accessibilityLabel={`Open ${docsTitle(r.title)}`}
              onPress={() => ctx.openRow(r.id)}
              style={({ pressed }) => ({ width: cardW, borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, overflow: "hidden", backgroundColor: pressed ? c.sunken : c.surface })}
            >
              <Preview ctx={ctx} row={r} height={previewH} />
              <View style={{ padding: s[3], gap: s[2] }}>
                <Text role="label" weight={600} numberOfLines={2}>
                  {r.icon ? `${r.icon} ` : ""}
                  {docsTitle(r.title)}
                </Text>
                <RowValues ctx={ctx} row={r} />
              </View>
            </Pressable>
          ))}
        </View>
      )}
    />
  );
}
