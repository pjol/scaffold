import {
  SUPPORT_ATTACHMENT_PURPOSE,
  SUPPORT_BODY_MAX,
  SUPPORT_CATEGORIES,
  SUPPORT_CATEGORY_LABEL,
  SUPPORT_MAX_ATTACHMENT_BYTES,
  SUPPORT_MAX_ATTACHMENTS,
  SUPPORT_SUBJECT_MAX,
  errorMessage,
  hasErrorCode,
  useCreateTicket,
  useUpload,
  type SupportAttachmentInput,
  type SupportCategory,
} from "@scaffoldapp/shared";
import { Banner, Button, Field, Input, PhotoUploader, Screen, TextArea, ToggleChip, toast, useTheme, type UploadedPhoto } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { useRef, useState } from "react";
import { View } from "react-native";
import { HeaderClose } from "@/components/HeaderClose";

/** New request (modal): category, subject, message and up to 5 photos. */
export function NewTicketScreen() {
  const theme = useTheme();
  const s = theme.space;
  const create = useCreateTicket();
  const upload = useUpload();
  const [category, setCategory] = useState<SupportCategory | null>(null);
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [photos, setPhotos] = useState<UploadedPhoto[]>([]);
  const [uploading, setUploading] = useState(false);
  const [tried, setTried] = useState(false);
  // Name and size of each uploaded key, for the attachment list staff see.
  const meta = useRef(new Map<string, { name: string; size: number }>());

  const errors = {
    category: category ? null : "Choose a category.",
    subject: subject.trim().length < 3 ? "Write at least 3 characters." : null,
    body: body.trim() ? null : "Describe what happened.",
  };
  const valid = !errors.category && !errors.subject && !errors.body;

  const submit = () => {
    setTried(true);
    if (!valid || uploading || !category) return;
    const attachments: SupportAttachmentInput[] = photos.map((p, i) => {
      const m = meta.current.get(p.key);
      return { key: p.key, name: m?.name ?? `photo-${i + 1}.jpg`, size: m?.size ?? 0 };
    });
    create.mutate(
      { category, subject: subject.trim(), body: body.trim(), attachments },
      {
        onSuccess: (t) => {
          toast.success("Request sent");
          router.replace({ pathname: "/settings/support-requests/[id]", params: { id: t.id } });
        },
      },
    );
  };

  return (
    <Screen
      edges={["bottom"]}
      keyboard
      gap={s[6]}
      footer={
        <Button fullWidth loading={create.isPending} disabled={uploading} onPress={submit}>
          Send request
        </Button>
      }
    >
      <Stack.Screen options={{ title: "New request", presentation: "modal", headerLeft: () => <HeaderClose /> }} />
      {create.isError ? (
        <Banner
          tone="danger"
          title={hasErrorCode(create.error, "rate_limited") ? "You've sent several requests in the last hour. Try again later." : errorMessage(create.error, "Couldn't send your request. Try again.")}
        />
      ) : null}
      <Field label="Category" required error={tried ? errors.category : null}>
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
          {SUPPORT_CATEGORIES.map((c) => (
            <ToggleChip key={c} pressed={category === c} onPressedChange={(on) => setCategory(on ? c : null)}>
              {SUPPORT_CATEGORY_LABEL[c]}
            </ToggleChip>
          ))}
        </View>
      </Field>
      <Field label="Subject" required error={tried ? errors.subject : null}>
        <Input value={subject} onChangeText={setSubject} maxLength={SUPPORT_SUBJECT_MAX} invalid={tried && !!errors.subject} returnKeyType="next" />
      </Field>
      <Field label="Message" required error={tried ? errors.body : null} info="Include what you were doing and what you expected.">
        <TextArea value={body} onChangeText={setBody} maxLength={SUPPORT_BODY_MAX} invalid={tried && !!errors.body} />
      </Field>
      <Field label="Photos" info={`Up to ${SUPPORT_MAX_ATTACHMENTS} screenshots or photos.`}>
        <PhotoUploader
          max={SUPPORT_MAX_ATTACHMENTS}
          sources={["library"]}
          onChange={setPhotos}
          onUploadingChange={setUploading}
          upload={async (file, { signal }) => {
            const r = await upload.mutateAsync({ file, purpose: SUPPORT_ATTACHMENT_PURPOSE, maxBytes: SUPPORT_MAX_ATTACHMENT_BYTES, signal });
            meta.current.set(r.key, { name: file.name || `photo-${meta.current.size + 1}.jpg`, size: r.size });
            return { key: r.key };
          }}
        />
      </Field>
    </Screen>
  );
}
