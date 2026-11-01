"use client";

import {
  assistantConversationTitle,
  assistantLimitReached,
  errorMessage,
  hasErrorCode,
  useAssistantChat,
  useAssistantConversation,
  useAssistantMessages,
  useAssistantStatus,
  useCreateAssistantConversation,
  useDeleteAssistantConversation,
  useRenameAssistantConversation,
  type AssistantMessage,
} from "@scaffoldapp/shared";
import {
  Banner,
  Button,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  Menu,
  MenuItem,
  MenuSeparator,
  Skeleton,
  SkeletonGroup,
  toast,
} from "@scaffoldapp/ui-web";
import { ChevronLeft, MoreHorizontal, Pencil, Sparkles, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Composer } from "./Composer";
import { ConsentDialog } from "./ConsentDialog";
import { MessageItem } from "./MessageItem";
import { QuotaBanner } from "./QuotaBanner";

/**
 * One conversation: messages (oldest at the top, earlier pages on demand), the composer,
 * and the states around them: consent before the first message, daily limit, provider not
 * configured, development mock. Without `conversationId` it is a new conversation that is
 * created on the first message (the URL then moves to /assistant/<id> without a reload).
 */
export function Thread({ conversationId, onCreated }: { conversationId: string | undefined; onCreated: (id: string) => void }) {
  const status = useAssistantStatus();
  const conversation = useAssistantConversation(conversationId);
  const messages = useAssistantMessages(conversationId);
  const chat = useAssistantChat(conversationId);
  const create = useCreateAssistantConversation();
  const [consentOpen, setConsentOpen] = useState(false);
  const [draft, setDraft] = useState("");
  const pending = useRef<string | null>(null);

  const ordered = useMemo(() => [...(messages.data?.items ?? [])].reverse(), [messages.data]);
  const lastUserText = useMemo(() => {
    for (let i = ordered.length - 1; i >= 0; i--) {
      const m = ordered[i];
      if (m?.role === "user") return m.parts.map((p) => p.text).join("");
    }
    return null;
  }, [ordered]);

  // Consent required at send time (or a stale status): ask, then send what was typed.
  useEffect(() => {
    if (chat.error?.code === "ai_consent_required") {
      void status.refetch();
      setConsentOpen(true);
      chat.clearError();
    }
  }, [chat, status]);

  // `consented` skips the check right after the dialog (this render's status is stale).
  const send = async (text: string, consented = false): Promise<boolean> => {
    if (!status.data?.available) return false;
    if (status.data.consent.required && !consented) {
      pending.current = text;
      setConsentOpen(true);
      return false;
    }
    let id = conversationId;
    if (!id) {
      try {
        id = (await create.mutateAsync()).id;
        onCreated(id);
      } catch (e) {
        toast.error(errorMessage(e, "Couldn't start a conversation. Try again."));
        return false;
      }
    }
    pending.current = text;
    // The message is posted as soon as the stream opens; clear the box then, not at the end.
    setDraft("");
    const ok = await chat.send(text, id);
    if (ok) pending.current = null;
    else if (pending.current === text) setDraft((d) => d || text);
    return ok;
  };

  // Scroll: stick to the bottom while the user is there.
  const scroller = useRef<HTMLDivElement>(null);
  const atBottom = useRef(true);
  const onScroll = () => {
    const el = scroller.current;
    if (el) atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
  };
  const lastText = ordered.at(-1)?.parts[0]?.text.length ?? 0;
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && atBottom.current) el.scrollTop = el.scrollHeight;
  }, [ordered.length, lastText, ordered.at(-1)?.activity.length]);

  if (status.isPending || (conversationId && (conversation.isPending || messages.isPending))) return <ThreadSkeleton />;
  if (status.isError) return <ErrorState title="Couldn't load the assistant" onRetry={() => void status.refetch()} retrying={status.isFetching} />;
  if (conversationId && conversation.isError) {
    return hasErrorCode(conversation.error, "not_found") ? (
      <EmptyState
        icon={<Sparkles />}
        title="This conversation doesn't exist"
        action={
          <Button asChild variant="secondary">
            <Link href="/assistant/new">New conversation</Link>
          </Button>
        }
      />
    ) : (
      <ErrorState title="Couldn't load this conversation" onRetry={() => void conversation.refetch()} retrying={conversation.isFetching} />
    );
  }
  const st = status.data;
  if (!st.available || !st.provider) {
    return <EmptyState icon={<Sparkles />} title="The assistant isn't available" info="The server has no AI provider configured." />;
  }
  const limited = assistantLimitReached(st.usage) !== null || chat.error?.code === "quota_exceeded";

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <ThreadHeader conversationId={conversationId} title={assistantConversationTitle(conversation.data)} />
      <div ref={scroller} onScroll={onScroll} className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 py-6">
          {st.mock ? <Banner tone="warning">Development mock: replies are scripted and no AI provider is called.</Banner> : null}
          {messages.hasNextPage ? (
            <Button variant="ghost" size="sm" className="self-center" loading={messages.isFetchingNextPage} onClick={() => void messages.fetchNextPage()}>
              Show earlier messages
            </Button>
          ) : null}
          {messages.isError ? <ErrorState compact title="Couldn't load messages" onRetry={() => void messages.refetch()} retrying={messages.isFetching} /> : null}
          {ordered.length === 0 && !messages.isError ? <EmptyState icon={<Sparkles />} title="Ask the assistant" tone="primary" /> : null}
          {ordered.map((m: AssistantMessage, i) => (
            <MessageItem
              key={m.id}
              message={m}
              onRetry={i === ordered.length - 1 && lastUserText && !chat.streaming ? () => void send(lastUserText) : undefined}
            />
          ))}
        </div>
      </div>
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-2 pt-2 pb-4">
        {limited ? <QuotaBanner usage={st.usage} /> : null}
        {chat.error && chat.error?.code !== "quota_exceeded" && chat.error?.code !== "ai_consent_required" ? (
          <Banner
            tone="danger"
            action={
              <Button size="sm" variant="ghost" onClick={chat.clearError}>
                Dismiss
              </Button>
            }
          >
            {chat.error.message}
          </Banner>
        ) : null}
        <Composer
          value={draft}
          onChange={setDraft}
          onSend={send}
          onStop={() => void chat.stop().catch((e: unknown) => toast.error(errorMessage(e, "Couldn't stop the reply. Try again.")))}
          streaming={chat.streaming}
          disabled={limited}
          maxChars={st.max_message_chars}
        />
      </div>
      <ConsentDialog
        provider={st.provider}
        open={consentOpen}
        onOpenChange={setConsentOpen}
        onAccepted={() => {
          const text = pending.current;
          if (text) void send(text, true);
        }}
      />
    </div>
  );
}

function ThreadHeader({ conversationId, title }: { conversationId: string | undefined; title: string }) {
  const router = useRouter();
  const rename = useRenameAssistantConversation();
  const remove = useDeleteAssistantConversation();
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [draft, setDraft] = useState(title);

  return (
    <div className="flex min-h-control-lg items-center gap-2 border-b border-border pb-2">
      <Link
        href="/assistant"
        aria-label="Conversations"
        className="-ml-2 flex size-control shrink-0 items-center justify-center rounded-pill text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring lg:hidden pointer-coarse:size-tap"
      >
        <ChevronLeft className="size-5" aria-hidden />
      </Link>
      <h2 className="min-w-0 flex-1 truncate type-title-sm text-fg">{title}</h2>
      {conversationId ? (
        <Menu align="end" trigger={<IconButton label="Conversation options" variant="ghost" icon={<MoreHorizontal />} />}>
          <MenuItem
            icon={<Pencil />}
            onSelect={() => {
              setDraft(title === "New conversation" ? "" : title);
              setRenaming(true);
            }}
          >
            Rename
          </MenuItem>
          <MenuSeparator />
          <MenuItem icon={<Trash2 />} destructive onSelect={() => setDeleting(true)}>
            Delete
          </MenuItem>
        </Menu>
      ) : null}
      {conversationId ? (
        <>
          <Dialog
            open={renaming}
            onOpenChange={setRenaming}
            title="Rename conversation"
            size="sm"
            footer={
              <>
                <Button variant="secondary" onClick={() => setRenaming(false)}>
                  Cancel
                </Button>
                <Button
                  loading={rename.isPending}
                  disabled={!draft.trim()}
                  onClick={() =>
                    rename.mutate(
                      { id: conversationId, title: draft.trim() },
                      { onSuccess: () => setRenaming(false), onError: (e) => toast.error(errorMessage(e, "Couldn't rename. Try again.")) },
                    )
                  }
                >
                  Save
                </Button>
              </>
            }
          >
            <Field label="Title">
              <Input value={draft} maxLength={120} onChange={(e) => setDraft(e.target.value)} autoFocus />
            </Field>
          </Dialog>
          <ConfirmDialog
            open={deleting}
            onOpenChange={setDeleting}
            title="Delete this conversation?"
            confirmLabel="Delete"
            tone="danger"
            onConfirm={async () => {
              await remove.mutateAsync(conversationId);
              toast.success("Conversation deleted");
              router.replace("/assistant");
            }}
            errorText={(e) => errorMessage(e, "Couldn't delete. Try again.")}
          >
            It can't be undone.
          </ConfirmDialog>
        </>
      ) : null}
    </div>
  );
}

function ThreadSkeleton() {
  return (
    <SkeletonGroup className="flex min-h-0 flex-1 flex-col gap-6 pt-2">
      <Skeleton className="h-8 w-48" />
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-6">
        <Skeleton className="h-12 w-2/3 self-end rounded-xl" />
        <Skeleton shape="text" className="h-4 w-full" />
        <Skeleton shape="text" className="h-4 w-5/6" />
        <Skeleton className="h-12 w-1/2 self-end rounded-xl" />
      </div>
    </SkeletonGroup>
  );
}
