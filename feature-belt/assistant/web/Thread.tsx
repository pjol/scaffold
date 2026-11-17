"use client";

import {
  assistantContextChipText,
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
  type AssistantContextLaunch,
  type AssistantContextRequest,
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
import { ChevronLeft, MoreHorizontal, Paperclip, Pencil, Sparkles, Trash2, X } from "lucide-react";
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
 * `launch` attaches a resource (Summarise sends at once; Ask waits for the question) and
 * `question` sends a question at once (⌘K "Ask the assistant"). `variant="panel"` drops the
 * back link for the side panel.
 */
export function Thread({
  conversationId,
  onCreated,
  launch,
  question,
  variant = "page",
}: {
  conversationId: string | undefined;
  onCreated: (id: string) => void;
  launch?: AssistantContextLaunch | null | undefined;
  question?: string | null | undefined;
  variant?: "page" | "panel";
}) {
  const status = useAssistantStatus();
  const conversation = useAssistantConversation(conversationId);
  const messages = useAssistantMessages(conversationId);
  const chat = useAssistantChat(conversationId);
  const create = useCreateAssistantConversation();
  const [consentOpen, setConsentOpen] = useState(false);
  const [draft, setDraft] = useState("");
  const pending = useRef<string | null>(null);
  // The resource the next message attaches (cleared once it is sent: it is in the history then).
  const [attached, setAttached] = useState<(AssistantContextRequest & { label?: string | undefined }) | null>(
    launch ? { ...launch.context, label: launch.label } : null,
  );
  const pendingContext = useRef<AssistantContextRequest | null>(null);

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
    const ctx = pendingContext.current ?? (attached ? { kind: attached.kind, id: attached.id, ...(attached.scope ? { scope: attached.scope } : {}), ...(attached.title ? { title: attached.title } : {}) } : null);
    if (status.data.consent.required && !consented) {
      pending.current = text;
      pendingContext.current = ctx;
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
    const ok = await chat.send(text, id, ctx ?? undefined);
    if (ok) {
      pending.current = null;
      pendingContext.current = null;
      setAttached(null);
    } else if (pending.current === text) setDraft((d) => d || text);
    return ok;
  };

  // Summarise (or a question from ⌘K) sends once, as soon as the assistant is ready.
  const autoSent = useRef(false);
  const autoText = launch?.mode === "summarise" ? (launch.prompt ?? null) : (question ?? null);
  useEffect(() => {
    if (autoSent.current || !autoText || !status.data?.available || conversationId) return;
    autoSent.current = true;
    void send(autoText);
    // Runs once when the status first allows it.
  }, [status.data?.available, autoText, conversationId]);
  useEffect(() => {
    if (launch?.mode === "ask" && question && !autoText) setDraft(question);
  }, []);

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
      <ThreadHeader conversationId={conversationId} title={assistantConversationTitle(conversation.data)} variant={variant} />
      <div ref={scroller} onScroll={onScroll} className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 py-6">
          {st.mock ? <Banner tone="warning">Development mock: replies are scripted and no AI provider is called.</Banner> : null}
          {messages.hasNextPage ? (
            <Button variant="ghost" size="sm" className="self-center" loading={messages.isFetchingNextPage} onClick={() => void messages.fetchNextPage()}>
              Show earlier messages
            </Button>
          ) : null}
          {messages.isError ? <ErrorState compact title="Couldn't load messages" onRetry={() => void messages.refetch()} retrying={messages.isFetching} /> : null}
          {ordered.length === 0 && !messages.isError && !autoText ? <EmptyState icon={<Sparkles />} title={attached ? "Ask about it" : "Ask the assistant"} tone="primary" /> : null}
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
        {attached ? (
          <div className="flex items-center gap-2 self-start rounded-pill border border-border bg-surface py-1 pr-1 pl-3 type-label text-fg">
            <Paperclip aria-hidden className="size-4 shrink-0 text-fg-muted" />
            <span className="max-w-72 truncate">{assistantContextChipText({ label: attached.label ?? "", title: attached.title ?? "", scope_label: "" })}</span>
            <IconButton size="sm" variant="ghost" label="Don't attach it" icon={<X />} onClick={() => setAttached(null)} />
          </div>
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

function ThreadHeader({ conversationId, title, variant }: { conversationId: string | undefined; title: string; variant: "page" | "panel" }) {
  const router = useRouter();
  const rename = useRenameAssistantConversation();
  const remove = useDeleteAssistantConversation();
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [draft, setDraft] = useState(title);

  return (
    <div className="flex min-h-control-lg items-center gap-2 border-b border-border pb-2">
      {variant === "page" ? (
        <Link
          href="/assistant"
          aria-label="Conversations"
          className="-ml-2 flex size-control shrink-0 items-center justify-center rounded-pill text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring lg:hidden pointer-coarse:size-tap"
        >
          <ChevronLeft className="size-5" aria-hidden />
        </Link>
      ) : null}
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
