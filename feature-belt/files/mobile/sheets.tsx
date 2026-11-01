import {
  errorMessage,
  filesCanEdit,
  useFilesChildren,
  useFilesCreateFolder,
  useFilesMove,
  useFilesCopy,
  useFilesRename,
  useFilesSection,
  type FilesItem,
} from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, Field, Input, ListGroup, ListRow, Sheet, SkeletonRow, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { ArrowLeft, Folder, HardDrive, Users } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Pressable, View } from "react-native";

export function NewFolderSheet({ orgId, parentId, open, onClose }: { orgId: string; parentId: string | null; open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const create = useFilesCreateFolder(orgId);
  const [name, setName] = useState("Untitled folder");
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (open) {
      setName("Untitled folder");
      setError(null);
    }
  }, [open]);
  const submit = () =>
    create.mutate(
      { parent_id: parentId, name: name.trim() },
      {
        onSuccess: (it) => {
          toast.success(`“${it.name}” created`);
          onClose();
        },
        onError: (e) => setError(errorMessage(e, "Couldn't create the folder. Try again.")),
      },
    );
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title="New folder"
      footer={
        <Button fullWidth loading={create.isPending} disabled={!name.trim()} onPress={submit}>
          Create
        </Button>
      }
    >
      <View style={{ gap: theme.space[4] }}>
        <Field label="Name" error={error}>
          <Input value={name} onChangeText={setName} maxLength={255} autoFocus selectTextOnFocus onSubmitEditing={submit} />
        </Field>
      </View>
    </Sheet>
  );
}

export function RenameSheet({ item, onClose }: { item: FilesItem | null; onClose: () => void }) {
  const rename = useFilesRename();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (item) {
      setName(item.name);
      setError(null);
    }
  }, [item]);
  const submit = () => {
    if (!item) return;
    if (!name.trim() || name.trim() === item.name) return onClose();
    rename.mutate(
      { id: item.id, name: name.trim() },
      {
        onSuccess: () => {
          toast.success("Renamed");
          onClose();
        },
        onError: (e) => setError(errorMessage(e, "Couldn't rename. Try again.")),
      },
    );
  };
  return (
    <Sheet
      open={item !== null}
      onClose={onClose}
      title="Rename"
      footer={
        <Button fullWidth loading={rename.isPending} onPress={submit}>
          Save
        </Button>
      }
    >
      <Field label="Name" error={error}>
        <Input value={name} onChangeText={setName} maxLength={255} autoFocus onSubmitEditing={submit} />
      </Field>
    </Sheet>
  );
}

type Place = { kind: "root" } | { kind: "my" } | { kind: "shared" } | { kind: "folder"; folder: FilesItem; trail: FilesItem[] };

/** Move or copy one item: My drive, or any folder you can edit. */
export function MoveSheet({ orgId, item, mode, onClose }: { orgId: string; item: FilesItem | null; mode: "move" | "copy"; onClose: () => void }) {
  const theme = useTheme();
  const [place, setPlace] = useState<Place>({ kind: "root" });
  const move = useFilesMove();
  const copy = useFilesCopy();
  const my = useFilesSection(place.kind === "my" && item ? orgId : null, "my");
  const shared = useFilesSection(place.kind === "shared" && item ? orgId : null, "shared");
  const children = useFilesChildren(place.kind === "folder" ? place.folder.id : null);
  const list = place.kind === "my" ? my : place.kind === "shared" ? shared : children;
  const folders = (list.data?.items ?? []).filter((i) => i.type === "folder" && i.id !== item?.id);
  useEffect(() => {
    if (item) setPlace({ kind: "root" });
  }, [item]);
  const target: { id: string | null; label: string } | null =
    place.kind === "my" ? { id: null, label: "My drive" } : place.kind === "folder" && filesCanEdit(place.folder) ? { id: place.folder.id, label: place.folder.name } : null;
  const busy = move.isPending || copy.isPending;
  const submit = () => {
    if (!item || !target) return;
    const done = (name: string) => {
      toast.success(mode === "copy" ? `Copied as “${name}”` : `Moved to ${target.label}`);
      onClose();
    };
    const fail = (e: unknown) => toast.error(errorMessage(e, mode === "copy" ? "Couldn't copy." : "Couldn't move."));
    if (mode === "copy") copy.mutate({ id: item.id, parentId: target.id }, { onSuccess: (it) => done(it.name), onError: fail });
    else move.mutate({ id: item.id, parentId: target.id }, { onSuccess: (it) => done(it.name), onError: fail });
  };
  const back = () => {
    if (place.kind === "folder") {
      const parent = place.trail[place.trail.length - 1];
      setPlace(parent ? { kind: "folder", folder: parent, trail: place.trail.slice(0, -1) } : place.folder.drive === "shared" ? { kind: "shared" } : { kind: "my" });
    } else setPlace({ kind: "root" });
  };
  return (
    <Sheet
      open={item !== null}
      onClose={onClose}
      title={item ? `${mode === "copy" ? "Copy" : "Move"} “${item.name}”` : undefined}
      snapPoints={["75%"]}
      footer={
        <Button fullWidth loading={busy} disabled={!target} onPress={submit}>
          {target ? `${mode === "copy" ? "Copy" : "Move"} to ${target.label}` : "Choose a folder"}
        </Button>
      }
    >
      <View style={{ gap: theme.space[3] }}>
        {place.kind !== "root" ? (
          <Pressable accessibilityRole="button" onPress={back} hitSlop={8} style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], minHeight: theme.tap.base }}>
            <ArrowLeft size={18} color={theme.colors.textMuted} />
            <Text role="label" tone="muted" weight={600}>
              Back
            </Text>
          </Pressable>
        ) : null}
        <ListGroup>
          {place.kind === "root" ? (
            <>
              <ListRow title="My drive" leading={<HardDrive size={22} color={theme.colors.textMuted} />} onPress={() => setPlace({ kind: "my" })} />
              <ListRow title="Shared folders" leading={<Users size={22} color={theme.colors.textMuted} />} onPress={() => setPlace({ kind: "shared" })} />
            </>
          ) : list.isPending ? (
            <View accessibilityLabel="Loading" accessibilityRole="progressbar">
              <SkeletonRow />
              <SkeletonRow />
            </View>
          ) : list.isError ? (
            <ErrorState compact title="Couldn't load folders" onRetry={() => void list.refetch()} />
          ) : folders.length === 0 ? (
            <EmptyState compact icon={Folder} title="No folders here" />
          ) : (
            folders.map((f) => (
              <ListRow
                key={f.id}
                title={f.name}
                leading={<Folder size={22} color={theme.colors.primaryStrong} />}
                onPress={() => setPlace({ kind: "folder", folder: f, trail: place.kind === "folder" ? [...place.trail, place.folder] : [] })}
              />
            ))
          )}
        </ListGroup>
      </View>
    </Sheet>
  );
}
