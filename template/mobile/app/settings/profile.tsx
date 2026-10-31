import { MAX_UPLOAD_BYTES, errorMessage, useMe, useUpdateMe, useUpload } from "@scaffoldapp/shared";
import { Avatar, Button, ErrorState, Field, Input, ListRow, Screen, Sheet, Skeleton, Spinner, haptic, pickPhotos, toast, useTheme } from "@scaffoldapp/ui-native";
import { Camera, Images, Mail, Save, Trash2 } from "lucide-react-native";
import { useEffect, useRef, useState } from "react";
import { Pressable, View } from "react-native";

type PhotoAction = "library" | "camera" | "remove";

/** Name and photo. The photo saves as soon as it uploads; Save applies the name. */
export default function ProfileScreen() {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const me = useMe();
  const update = useUpdateMe();
  const upload = useUpload();
  const [name, setName] = useState("");
  const [sheet, setSheet] = useState(false);
  const [photoBusy, setPhotoBusy] = useState(false);
  const pending = useRef<PhotoAction | null>(null);

  const loadedId = me.data?.id;
  useEffect(() => {
    if (me.data) setName(me.data.display_name);
    // Seed the field once per account, not on every refetch.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadedId]);

  const trimmed = name.trim();
  const nameError = trimmed.length === 0 ? "Enter your name." : trimmed.length > 60 ? "Use 60 characters or fewer." : null;
  const dirty = Boolean(me.data) && trimmed !== me.data?.display_name;

  const runPhoto = async (action: PhotoAction) => {
    setPhotoBusy(true);
    try {
      if (action === "remove") {
        await update.mutateAsync({ avatar_url: "" });
        toast.success("Photo removed");
        return;
      }
      const [photo] = await pickPhotos(action, 1, 0.8);
      if (!photo) return;
      const res = await upload.mutateAsync({ file: photo, purpose: "avatar", maxBytes: MAX_UPLOAD_BYTES });
      if (!res.public_url) throw new Error("Upload failed. Try again.");
      await update.mutateAsync({ avatar_url: res.public_url });
      haptic.success();
      toast.success("Photo updated");
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't update your photo. Try again."));
    } finally {
      setPhotoBusy(false);
    }
  };

  const choose = (action: PhotoAction) => {
    pending.current = action;
    setSheet(false);
  };

  const save = async () => {
    if (nameError || !dirty) return;
    try {
      await update.mutateAsync({ display_name: trimmed });
      haptic.success();
      toast.success("Profile saved");
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't save. Check your connection and try again."));
    }
  };

  const displayName = trimmed || me.data?.email || "Profile";

  return (
    <Screen
      keyboard
      edges={["bottom"]}
      gap={s[6]}
      footer={
        me.data ? (
          <Button fullWidth leftIcon={Save} disabled={!dirty || Boolean(nameError) || photoBusy} loading={update.isPending && !photoBusy} onPress={() => void save()}>
            Save
          </Button>
        ) : undefined
      }
    >
      {me.isPending ? (
        <View style={{ gap: s[6] }} accessibilityLabel="Loading" accessibilityRole="progressbar">
          <Skeleton shape="circle" width={96} height={96} style={{ alignSelf: "center" }} />
          <View style={{ gap: s[2] }}>
            <Skeleton shape="text" width={80} height={20} />
            <Skeleton height={theme.tap.base} />
          </View>
        </View>
      ) : !me.data ? (
        <ErrorState title="Couldn't load your profile" info={errorMessage(me.error)} onRetry={() => void me.refetch()} retrying={me.isRefetching} />
      ) : (
        <>
          <View style={{ alignItems: "center" }}>
            <Pressable accessibilityRole="button" accessibilityLabel="Change photo" disabled={photoBusy} onPress={() => setSheet(true)}>
              <Avatar name={displayName} src={me.data.avatar_url} size="2xl" />
              <View
                style={{
                  position: "absolute",
                  right: 0,
                  bottom: 0,
                  width: s[10],
                  height: s[10],
                  borderRadius: s[5],
                  backgroundColor: c.primary,
                  borderWidth: 2,
                  borderColor: c.canvas,
                  alignItems: "center",
                  justifyContent: "center",
                }}
              >
                {photoBusy ? <Spinner size={18} color={c.primaryFg} /> : <Camera size={18} color={c.primaryFg} strokeWidth={2.25} />}
              </View>
            </Pressable>
          </View>
          <View style={{ gap: s[4] }}>
            <Field label="Name" error={dirty || trimmed.length === 0 ? nameError : null}>
              <Input value={name} onChangeText={setName} autoComplete="name" textContentType="name" maxLength={80} returnKeyType="done" onSubmitEditing={() => void save()} />
            </Field>
            <Field label="Email" info="Your sign-in email.">
              <Input value={me.data.email} editable={false} leadingIcon={Mail} />
            </Field>
          </View>
        </>
      )}
      <Sheet
        open={sheet}
        onClose={() => setSheet(false)}
        onDismissed={() => {
          const action = pending.current;
          pending.current = null;
          // Let the sheet's modal finish dismissing before the system picker presents.
          if (action) setTimeout(() => void runPhoto(action), 100);
        }}
        title="Profile photo"
        snapPoints={[me.data?.avatar_url ? 300 : 240]}
      >
        <View style={{ marginHorizontal: -s[4] }}>
          <ListRow title="Choose photo" leading={<Images size={22} color={c.textMuted} strokeWidth={2} />} chevron={false} onPress={() => choose("library")} />
          <ListRow title="Take photo" leading={<Camera size={22} color={c.textMuted} strokeWidth={2} />} chevron={false} onPress={() => choose("camera")} />
          {me.data?.avatar_url ? <ListRow title="Remove photo" destructive leading={<Trash2 size={22} color={c.dangerStrong} strokeWidth={2} />} chevron={false} onPress={() => choose("remove")} /> : null}
        </View>
      </Sheet>
    </Screen>
  );
}
