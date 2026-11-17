import { useChannelsEmoji, type ChannelsCustomEmoji } from "@scaffoldapp/shared";
import { Text, type TextProps } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { createContext, useContext, useMemo, type ReactNode } from "react";

interface EmojiState {
  byName: Map<string, ChannelsCustomEmoji>;
  list: ChannelsCustomEmoji[];
  frequent: string[];
}

const EmojiContext = createContext<EmojiState>({ byName: new Map(), list: [], frequent: [] });

/** Makes the workspace's custom emoji available to messages, reactions and pickers below it. */
export function ChannelsEmojiProvider({ orgId, children }: { orgId: string | null | undefined; children: ReactNode }) {
  const emoji = useChannelsEmoji(orgId);
  const value = useMemo<EmojiState>(() => {
    const list = emoji.data?.items ?? [];
    return { byName: new Map(list.map((e) => [e.name, e])), list, frequent: emoji.data?.frequent ?? [] };
  }, [emoji.data]);
  return <EmojiContext.Provider value={value}>{children}</EmojiContext.Provider>;
}

export function useWorkspaceEmoji(): EmojiState {
  return useContext(EmojiContext);
}

/** A custom emoji image sized to the text around it, or its :name: when the workspace doesn't have it. */
export function CustomEmoji({ name, size = 20 }: { name: string; size?: number }) {
  const { byName } = useWorkspaceEmoji();
  const e = byName.get(name);
  if (!e) return <Text role="body">:{name}:</Text>;
  return <Image source={{ uri: e.url }} accessibilityLabel={`:${name}:`} contentFit="contain" style={{ width: size, height: size }} />;
}

/** A reaction or picker glyph: unicode, or `:name:` for custom emoji. */
export function EmojiGlyph({ value, size = 18, role = "label" }: { value: string; size?: number; role?: TextProps["role"] }) {
  const m = /^:([a-z0-9_+-]{2,40}):$/.exec(value);
  if (m?.[1]) return <CustomEmoji name={m[1]} size={size} />;
  return <Text role={role}>{value}</Text>;
}
