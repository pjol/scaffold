"use client";

import { Camera, ImageOff, ImagePlus, Images, RotateCw, Upload, X } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useCallback, useEffect, useRef, useState, type DragEvent } from "react";
import { cn } from "../lib/cn";
import { useHoverCapable, useLatest } from "../lib/hooks";
import { useMotionTokens } from "../motion/tokens";
import { Input } from "./Input";
import { toast } from "./Toaster";

/** What `upload` resolves to. Wire it to `useUpload().mutateAsync({ file, purpose })`. */
export interface PhotoUploadResult {
  /** Storage key to send to the owning endpoint. */
  key: string;
  /** Public URL for public purposes (else the local preview is kept). */
  url?: string | null;
}

export interface UploadedPhoto {
  id: string;
  key: string;
  url: string | null;
  /** Local object URL (until unmount) for an immediate preview elsewhere. */
  previewUrl?: string;
  caption?: string;
}

export interface PhotoUploaderProps {
  /** Upload one file. Report progress with `onProgress(0–1)` if you can; honour `signal` (removing a tile cancels). */
  upload: (file: File, ctx: { signal: AbortSignal; onProgress: (fraction: number) => void }) => Promise<PhotoUploadResult>;
  /** Photos already attached. */
  defaultValue?: UploadedPhoto[];
  /** Every uploaded photo, in order, whenever the set or a caption changes. */
  onChange?: (photos: UploadedPhoto[]) => void;
  /** True while any upload is in flight; disable submit meanwhile. */
  onUploadingChange?: (uploading: boolean) => void;
  /** @default 6 */
  max?: number;
  /** @default 10 MB (the backend's default limit) */
  maxSizeBytes?: number;
  /** @default "image/jpeg,image/png,image/heic,image/heif,image/webp" */
  accept?: string;
  /** Camera on phones (`capture`); `false` hides the camera button. @default "environment" */
  capture?: "environment" | "user" | false;
  /** Caption field under each photo. @default false */
  captions?: boolean;
  /** @default 200 */
  captionMaxLength?: number;
  /** Add-tile label. @default "Add photos" */
  label?: string;
  disabled?: boolean;
  className?: string;
}

interface Item {
  id: string;
  file: File | null;
  preview: string | null;
  status: "uploading" | "done" | "error";
  progress: number;
  reported: boolean;
  key: string | null;
  url: string | null;
  caption: string;
  broken: boolean;
  controller: AbortController | null;
}

const ACCEPT = "image/jpeg,image/png,image/heic,image/heif,image/webp";
const MB = 1024 * 1024;
let seq = 0;

function Ring({ value }: { value: number }) {
  const r = 17;
  const c = 2 * Math.PI * r;
  return (
    <svg viewBox="0 0 40 40" className="size-10 -rotate-90" aria-hidden>
      <circle cx="20" cy="20" r={r} fill="none" stroke="currentColor" strokeOpacity={0.28} strokeWidth={4} />
      <motion.circle
        cx="20"
        cy="20"
        r={r}
        fill="none"
        stroke="currentColor"
        strokeWidth={4}
        strokeLinecap="round"
        strokeDasharray={c}
        initial={false}
        animate={{ strokeDashoffset: c * (1 - Math.max(0.04, Math.min(1, value))) }}
        transition={{ type: "spring", stiffness: 120, damping: 24, mass: 1 }}
      />
    </svg>
  );
}

/**
 * Photo picker: camera + library on phones, drag and drop on desktop, instant previews,
 * per-photo progress, retry, remove (cancels the upload), a max count, optional captions.
 *
 * ```tsx
 * const upload = useUpload();
 * <PhotoUploader upload={(file, { signal }) => upload.mutateAsync({ file, purpose: "post_photo", signal }).then((r) => ({ key: r.key, url: r.public_url }))} onChange={setPhotos} />
 * ```
 */
export function PhotoUploader({
  upload,
  defaultValue,
  onChange,
  onUploadingChange,
  max = 6,
  maxSizeBytes = 10 * MB,
  accept = ACCEPT,
  capture = "environment",
  captions = false,
  captionMaxLength = 200,
  label = "Add photos",
  disabled = false,
  className,
}: PhotoUploaderProps) {
  const t = useMotionTokens();
  const hover = useHoverCapable();
  const [items, setItems] = useState<Item[]>(() =>
    (defaultValue ?? []).map((p) => ({ id: p.id, file: null, preview: p.previewUrl ?? null, status: "done", progress: 1, reported: true, key: p.key, url: p.url, caption: p.caption ?? "", broken: false, controller: null })),
  );
  const [dragging, setDragging] = useState(false);
  const libraryInput = useRef<HTMLInputElement>(null);
  const cameraInput = useRef<HTMLInputElement>(null);
  const uploadRef = useLatest(upload);
  const onChangeRef = useLatest(onChange);
  const onUploadingRef = useLatest(onUploadingChange);
  const urls = useRef(new Set<string>());
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    const set = urls.current;
    return () => {
      mounted.current = false;
      set.forEach((u) => URL.revokeObjectURL(u));
    };
  }, []);

  const patch = useCallback((id: string, p: Partial<Item>) => {
    if (mounted.current) setItems((prev) => prev.map((it) => (it.id === id ? { ...it, ...p } : it)));
  }, []);

  const doneSig = items
    .filter((i) => i.status === "done")
    .map((i) => `${i.id}:${i.key}:${i.caption}`)
    .join("|");
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    onChangeRef.current?.(
      items
        .filter((i): i is Item & { key: string } => i.status === "done" && !!i.key)
        .map((i) => ({ id: i.id, key: i.key, url: i.url, ...(i.preview ? { previewUrl: i.preview } : {}), ...(i.caption ? { caption: i.caption } : {}) })),
    );
    // Reports only when the set of finished photos changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [doneSig]);
  const uploading = items.some((i) => i.status === "uploading");
  useEffect(() => {
    onUploadingRef.current?.(uploading);
  }, [uploading, onUploadingRef]);

  // Uploads that don't report progress ease toward 90 %.
  const estimating = items.some((i) => i.status === "uploading" && !i.reported);
  useEffect(() => {
    if (!estimating) return undefined;
    const id = setInterval(() => {
      setItems((prev) => prev.map((i) => (i.status === "uploading" && !i.reported ? { ...i, progress: i.progress + (0.9 - i.progress) * 0.12 } : i)));
    }, 250);
    return () => clearInterval(id);
  }, [estimating]);

  const start = useCallback(
    (item: Item) => {
      if (!item.file) return;
      const controller = new AbortController();
      patch(item.id, { status: "uploading", progress: 0.03, reported: false, controller });
      uploadRef
        .current(item.file, { signal: controller.signal, onProgress: (f) => patch(item.id, { progress: Math.max(0.03, Math.min(1, f)), reported: true }) })
        .then((res) => {
          if (!controller.signal.aborted) patch(item.id, { status: "done", progress: 1, key: res.key, url: res.url ?? null, controller: null });
        })
        .catch(() => {
          if (!controller.signal.aborted) patch(item.id, { status: "error", controller: null });
        });
    },
    [patch, uploadRef],
  );

  const room = Math.max(0, max - items.length);

  const addFiles = (list: FileList | File[] | null) => {
    if (!list || disabled) return;
    const files = Array.from(list);
    const images = files.filter((f) => f.type.startsWith("image/") || /\.(heic|heif)$/i.test(f.name));
    if (images.length < files.length) toast.error("Only photos can be added.");
    const sized = images.filter((f) => f.size <= maxSizeBytes);
    if (sized.length < images.length) toast.error(`Photos must be under ${Math.round(maxSizeBytes / MB)} MB.`);
    if (sized.length > room) toast(`Up to ${max} photos.`);
    const fresh = sized.slice(0, room).map<Item>((file) => {
      const preview = URL.createObjectURL(file);
      urls.current.add(preview);
      return { id: `ph${++seq}`, file, preview, status: "uploading", progress: 0.03, reported: false, key: null, url: null, caption: "", broken: false, controller: null };
    });
    if (!fresh.length) return;
    setItems((prev) => [...prev, ...fresh]);
    fresh.forEach(start);
  };

  const remove = (id: string) => {
    setItems((prev) => {
      prev.find((i) => i.id === id)?.controller?.abort();
      return prev.filter((i) => i.id !== id);
    });
  };

  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setDragging(false);
    addFiles(e.dataTransfer.files);
  };

  const canAdd = room > 0 && !disabled;
  const tileBase =
    "relative flex aspect-square w-full flex-col items-center justify-center gap-1.5 rounded-lg border border-dashed border-border-strong type-label font-semibold text-fg-muted transition-colors [&_svg]:size-6";

  return (
    <div
      className={cn("relative", className)}
      onDragEnter={(e) => {
        if (!canAdd || !e.dataTransfer.types.includes("Files")) return;
        e.preventDefault();
        setDragging(true);
      }}
      onDragOver={(e) => {
        if (!canAdd) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = "copy";
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDragging(false);
      }}
      onDrop={onDrop}
    >
      <input
        ref={libraryInput}
        type="file"
        accept={accept}
        multiple={room > 1}
        className="sr-only"
        tabIndex={-1}
        aria-hidden
        onChange={(e) => {
          addFiles(e.target.files);
          e.target.value = "";
        }}
      />
      {capture && (
        <input
          ref={cameraInput}
          type="file"
          accept="image/*"
          capture={capture}
          className="sr-only"
          tabIndex={-1}
          aria-hidden
          onChange={(e) => {
            addFiles(e.target.files);
            e.target.value = "";
          }}
        />
      )}

      <ul className="grid grid-cols-3 gap-3 sm:grid-cols-4" aria-label="Photos">
        <AnimatePresence initial={false} mode="popLayout">
          {items.map((it) => (
            <motion.li
              key={it.id}
              layout
              initial={{ opacity: 0, scale: 0.6 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.6, transition: { duration: t.duration.fast } }}
              transition={t.spring.bouncy}
              className="flex flex-col gap-2"
            >
              <div className="relative aspect-square overflow-hidden rounded-lg bg-sunken">
                {(it.url ?? it.preview) && !it.broken ? (
                  <img src={it.preview ?? it.url ?? undefined} alt={it.caption || "Photo"} className="size-full object-cover" onError={() => patch(it.id, { broken: true })} draggable={false} />
                ) : (
                  <div className="flex size-full flex-col items-center justify-center gap-1 text-fg-muted [&_svg]:size-6">
                    <ImageOff />
                    <span className="max-w-full truncate px-2 type-caption">{it.file?.name ?? "Photo"}</span>
                  </div>
                )}
                <AnimatePresence>
                  {it.status !== "done" && (
                    <motion.div
                      key="overlay"
                      initial={{ opacity: 0 }}
                      animate={{ opacity: 1 }}
                      exit={{ opacity: 0 }}
                      className={cn("absolute inset-0 flex items-center justify-center", it.status === "error" ? "bg-danger/85" : "bg-ink-950/55")}
                    >
                      {it.status === "uploading" ? (
                        <span
                          className="relative flex items-center justify-center text-ink-50"
                          role="progressbar"
                          aria-label="Uploading"
                          aria-valuemin={0}
                          aria-valuemax={100}
                          aria-valuenow={Math.round(it.progress * 100)}
                        >
                          <Ring value={it.progress} />
                          <span className="absolute type-caption tabular-nums">{Math.round(it.progress * 100)}</span>
                        </span>
                      ) : (
                        <button type="button" onClick={() => start(it)} className="flex min-h-tap flex-col items-center justify-center gap-1 px-3 type-label font-semibold text-danger-fg [&_svg]:size-5">
                          <RotateCw />
                          Retry
                        </button>
                      )}
                    </motion.div>
                  )}
                </AnimatePresence>
                <button
                  type="button"
                  aria-label="Remove photo"
                  onClick={() => remove(it.id)}
                  disabled={disabled}
                  className="absolute top-1.5 right-1.5 flex size-8 items-center justify-center rounded-pill bg-ink-950/70 text-ink-50 transition-colors before:absolute before:-inset-1.5 before:content-[''] hover:bg-ink-950/85 disabled:opacity-50 [&_svg]:size-4"
                >
                  <X strokeWidth={2.5} />
                </button>
              </div>
              {captions && it.status === "done" && (
                <Input value={it.caption} maxLength={captionMaxLength} placeholder="Caption" aria-label="Photo caption" onChange={(e) => patch(it.id, { caption: e.target.value })} />
              )}
            </motion.li>
          ))}

          {canAdd && (
            <motion.li key="add" layout transition={t.spring.gentle} className={cn("grid gap-3", !hover && capture ? "col-span-2 grid-cols-2" : "")}>
              {!hover && capture ? (
                <>
                  <button type="button" onClick={() => cameraInput.current?.click()} className={cn(tileBase, "border-primary bg-primary-soft text-primary-strong active:scale-[0.97]")}>
                    <Camera />
                    Camera
                  </button>
                  <button type="button" onClick={() => libraryInput.current?.click()} className={cn(tileBase, "active:scale-[0.97]")}>
                    <Images />
                    Library
                  </button>
                </>
              ) : (
                <button
                  type="button"
                  onClick={() => libraryInput.current?.click()}
                  className={cn(tileBase, "hover:border-primary hover:bg-primary-soft hover:text-primary-strong")}
                  aria-label={`${label} (${items.length} of ${max})`}
                >
                  <ImagePlus />
                  <span className="px-2 text-center">{label}</span>
                  <span className="type-caption tabular-nums">
                    {items.length}/{max}
                  </span>
                </button>
              )}
            </motion.li>
          )}
        </AnimatePresence>
      </ul>

      <AnimatePresence>
        {dragging && (
          <motion.div
            initial={{ opacity: 0, scale: 0.98 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.98 }}
            transition={t.spring.snappy}
            className="pointer-events-none absolute -inset-2 z-(--app-z-raised) flex flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed border-primary bg-primary-soft text-primary-strong"
          >
            <Upload className="size-8" />
            <span className="type-title-sm">Drop to add</span>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}
