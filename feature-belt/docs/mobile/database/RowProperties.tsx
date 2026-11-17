import { errorMessage, useDocsPressButton, useDocsSetRowValues, type DocsPageDetail, type DocsProperty } from "@scaffoldapp/shared";
import { Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { useState } from "react";
import { Pressable, View } from "react-native";
import { PropertySheet } from "../PropertySheet";
import { openButtonResult } from "./open";
import { ValueView, valueLabel } from "./values";

/** A row page's properties: every type shown; tap to edit, toggle, run a button, or read a computed value. */
export function RowProperties({ detail, editable, token = null }: { detail: DocsPageDetail; editable: boolean; token?: string | null }) {
  const theme = useTheme();
  const s = theme.space;
  const db = detail.database;
  const page = detail.page;
  const setValues = useDocsSetRowValues(db?.id ?? "");
  const press = useDocsPressButton(db?.id ?? "");
  const [editing, setEditing] = useState<DocsProperty | null>(null);
  if (!db) return null;
  const canEdit = editable && !token;
  const openPage = (id: string) => router.push((token ? `/p/${token}?page=${id}` : `/docs/${id}`) as never);
  const tap = (p: DocsProperty) => {
    if (p.type === "button") {
      if (!canEdit) return;
      press.mutate({ rowId: page.id, propId: p.id }, { onSuccess: openButtonResult, onError: (e) => toast.error(errorMessage(e, "Couldn't run that button.")) });
      return;
    }
    if (p.type === "checkbox" && canEdit && !p.read_only) {
      setValues.mutate({ rowId: page.id, values: { [p.id]: page.props[p.id] !== true } }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save that value.")) });
      return;
    }
    setEditing(p);
  };
  return (
    <View style={{ gap: s[1], borderBottomWidth: 1, borderBottomColor: theme.colors.border, paddingBottom: s[3] }}>
      {db.schema
        .filter((p) => p.type !== "title")
        .map((p) => (
          <Pressable
            key={p.id}
            accessibilityRole="button"
            accessibilityLabel={valueLabel(p, page.props[p.id], detail.refs, page)}
            onPress={() => tap(p)}
            style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.base, backgroundColor: pressed ? theme.colors.sunken : "transparent" })}
          >
            <Text role="label" tone="muted" numberOfLines={2} style={{ width: 112 }}>
              {p.name}
            </Text>
            <View style={{ flex: 1, paddingVertical: s[1] }}>
              <ValueView
                prop={p}
                value={page.props[p.id]}
                refs={detail.refs}
                row={page}
                onOpenPage={openPage}
                pressing={press.isPending && press.variables?.propId === p.id}
                {...(p.type === "button" && canEdit ? { onPressButton: () => tap(p) } : {})}
              />
            </View>
          </Pressable>
        ))}
      <PropertySheet
        open={!!editing}
        prop={editing}
        value={editing ? page.props[editing.id] : undefined}
        orgId={page.org_id}
        refs={detail.refs}
        rowId={page.id}
        dbId={db.id}
        row={page}
        readOnly={!canEdit}
        token={token}
        onOpenPage={(id) => {
          setEditing(null);
          openPage(id);
        }}
        onClose={() => setEditing(null)}
        onSave={(v) => {
          const p = editing;
          setEditing(null);
          if (p && canEdit) setValues.mutate({ rowId: page.id, values: { [p.id]: v } }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save that value.")) });
        }}
      />
    </View>
  );
}
