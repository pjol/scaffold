"use client";

import { useLocalParticipant, useMediaDeviceSelect } from "@livekit/components-react";
import { CALLS_REACTIONS, type CallsReaction } from "@scaffoldapp/shared";
import { Badge, IconButton, Menu, MenuChoice, MenuItem, MenuLabel, MenuSeparator, MenuSub, Tooltip, cn, toast, useMediaQuery } from "@scaffoldapp/ui-web";
import {
  ChevronUp,
  Copy,
  Hand,
  LayoutGrid,
  MessageSquare,
  Mic,
  MicOff,
  MonitorUp,
  MonitorX,
  MoreVertical,
  PhoneOff,
  Settings2,
  SmilePlus,
  Users,
  Video,
  VideoOff,
} from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { mediaProblem, mediaProblemText, shortcutLabel } from "../media";
import { readDevicePrefs, writeDevicePrefs } from "../storage";
import type { CallLayout, CallPanel } from "./session";

export interface ControlsProps {
  panel: CallPanel;
  onPanel: (p: CallPanel) => void;
  layout: CallLayout;
  onLayout: (l: CallLayout) => void;
  unreadChat: number;
  /** People waiting in the lobby (hosts). */
  waiting: number;
  participants: number;
  isHost: boolean;
  handRaised: boolean;
  onToggleHand: () => void;
  onReact: (r: CallsReaction) => void;
  onCopyLink: () => void;
  onSettings: () => void;
  onLeave: () => void;
  onEndForAll: () => void;
}

/** Toggle the mic or camera; failures say what to do. */
export async function toggleMedia(kind: "mic" | "camera", setEnabled: (on: boolean) => Promise<unknown>, on: boolean) {
  try {
    await setEnabled(on);
  } catch (err) {
    toast.error(mediaProblemText(kind === "mic" ? "microphone" : "camera", mediaProblem(err)));
  }
}

const round = "size-control rounded-pill sm:size-control-lg";

/** The bar under the stage: media, share, hand, reactions, panels, more, leave. */
export function Controls(props: ControlsProps) {
  const { localParticipant, isMicrophoneEnabled, isCameraEnabled, isScreenShareEnabled } = useLocalParticipant();
  const wide = useMediaQuery("(min-width: 640px)");
  const canShare = typeof navigator !== "undefined" && !!navigator.mediaDevices && "getDisplayMedia" in navigator.mediaDevices;
  const [reacting, setReacting] = useState(false);

  const share = async () => {
    try {
      await localParticipant.setScreenShareEnabled(!isScreenShareEnabled, { audio: true, selfBrowserSurface: "exclude", surfaceSwitching: "include" });
    } catch (err) {
      // Closing the browser's picker rejects with NotAllowedError: not an error to report.
      if (mediaProblem(err) !== "denied") toast.error("Couldn't share your screen. Try again.");
    }
  };

  const mic = (
    <MediaToggle
      on={isMicrophoneEnabled}
      label={isMicrophoneEnabled ? "Turn off microphone" : "Turn on microphone"}
      shortcut={shortcutLabel("D")}
      icon={isMicrophoneEnabled ? <Mic /> : <MicOff />}
      onToggle={() => void toggleMedia("mic", (on) => localParticipant.setMicrophoneEnabled(on), !isMicrophoneEnabled)}
      menu={wide ? <DeviceMenu kind="audioinput" title="Microphone" /> : null}
    />
  );
  const cam = (
    <MediaToggle
      on={isCameraEnabled}
      label={isCameraEnabled ? "Turn off camera" : "Turn on camera"}
      shortcut={shortcutLabel("E")}
      icon={isCameraEnabled ? <Video /> : <VideoOff />}
      onToggle={() => void toggleMedia("camera", (on) => localParticipant.setCameraEnabled(on), !isCameraEnabled)}
      menu={wide ? <DeviceMenu kind="videoinput" title="Camera" /> : null}
    />
  );

  return (
    <div className="relative flex items-center justify-center gap-1.5 px-3 pt-2 pb-3 sm:gap-3" role="toolbar" aria-label="Call controls">
      {reacting && (
        <ReactionBar
          onPick={(r) => {
            props.onReact(r);
            setReacting(false);
          }}
          onClose={() => setReacting(false)}
        />
      )}
      {mic}
      {cam}
      {wide && canShare && (
        <Tooltip content={isScreenShareEnabled ? "Stop presenting" : "Present your screen"}>
          <IconButton
            label={isScreenShareEnabled ? "Stop presenting" : "Present your screen"}
            icon={isScreenShareEnabled ? <MonitorX /> : <MonitorUp />}
            variant={isScreenShareEnabled ? "primary" : "secondary"}
            className={round}
            onClick={() => void share()}
          />
        </Tooltip>
      )}
      {wide && (
        <Tooltip content={`${props.handRaised ? "Lower hand" : "Raise hand"} · ${shortcutLabel("H", true)}`}>
          <IconButton label={props.handRaised ? "Lower hand" : "Raise hand"} icon={<Hand />} variant={props.handRaised ? "primary" : "secondary"} className={round} aria-pressed={props.handRaised} onClick={props.onToggleHand} />
        </Tooltip>
      )}
      <Tooltip content="Send a reaction">
        <IconButton label="Send a reaction" icon={<SmilePlus />} variant={reacting ? "primary" : "secondary"} className={round} aria-expanded={reacting} onClick={() => setReacting((r) => !r)} />
      </Tooltip>
      <PanelButton
        label="People"
        shortcut={shortcutLabel("P", true)}
        icon={<Users />}
        active={props.panel === "people"}
        badge={props.isHost && props.waiting > 0 ? props.waiting : undefined}
        badgeTone="warning"
        count={props.participants}
        onClick={() => props.onPanel(props.panel === "people" ? null : "people")}
      />
      <PanelButton
        label="Chat"
        shortcut={shortcutLabel("C", true)}
        icon={<MessageSquare />}
        active={props.panel === "chat"}
        badge={props.unreadChat || undefined}
        onClick={() => props.onPanel(props.panel === "chat" ? null : "chat")}
      />
      <Menu side="top" trigger={<IconButton label="More options" icon={<MoreVertical />} variant="secondary" className={round} />}>
        {!wide && (
          <>
            <MenuItem icon={<Hand />} onSelect={props.onToggleHand}>
              {props.handRaised ? "Lower hand" : "Raise hand"}
            </MenuItem>
            {canShare && (
              <MenuItem icon={isScreenShareEnabled ? <MonitorX /> : <MonitorUp />} onSelect={() => void share()}>
                {isScreenShareEnabled ? "Stop presenting" : "Present your screen"}
              </MenuItem>
            )}
            <MenuSeparator />
          </>
        )}
        <MenuSub icon={<LayoutGrid />} label="Layout" hint={props.layout === "auto" ? "Auto" : props.layout === "grid" ? "Grid" : "Speaker"}>
          {(["auto", "grid", "speaker"] as const).map((l) => (
            <MenuChoice key={l} checked={props.layout === l} onSelect={() => props.onLayout(l)}>
              {l === "auto" ? "Auto" : l === "grid" ? "Grid" : "Speaker"}
            </MenuChoice>
          ))}
        </MenuSub>
        <MenuItem icon={<Copy />} onSelect={props.onCopyLink}>
          Copy meeting link
        </MenuItem>
        {props.isHost && (
          <MenuItem icon={<Settings2 />} onSelect={props.onSettings}>
            Meeting settings
          </MenuItem>
        )}
      </Menu>
      {props.isHost ? (
        <Menu side="top" trigger={<IconButton label="Leave call" icon={<PhoneOff />} variant="danger" className={cn(round, "w-14 bg-danger text-danger-fg hover:bg-danger sm:w-16")} />}>
          <MenuItem icon={<PhoneOff />} onSelect={props.onLeave}>
            Leave
          </MenuItem>
          <MenuItem icon={<PhoneOff />} destructive onSelect={props.onEndForAll}>
            End for everyone
          </MenuItem>
        </Menu>
      ) : (
        <Tooltip content="Leave call">
          <IconButton label="Leave call" icon={<PhoneOff />} variant="danger" className={cn(round, "w-14 bg-danger text-danger-fg hover:bg-danger sm:w-16")} onClick={props.onLeave} />
        </Tooltip>
      )}
    </div>
  );
}

function MediaToggle({ on, label, shortcut, icon, onToggle, menu }: { on: boolean; label: string; shortcut: string; icon: ReactNode; onToggle: () => void; menu: ReactNode }) {
  return (
    <div className={cn("flex items-center rounded-pill", on ? "bg-fg/8" : "bg-danger")}>
      {menu}
      <Tooltip content={`${label} · ${shortcut}`}>
        <IconButton label={label} icon={icon} variant={on ? "secondary" : "danger"} className={cn(round, on ? "" : "bg-danger text-danger-fg hover:bg-danger")} aria-pressed={on} onClick={onToggle} />
      </Tooltip>
    </div>
  );
}

/** Pick an input device (and, in the mic menu, the speaker where browsers allow it). */
function DeviceMenu({ kind, title }: { kind: "audioinput" | "videoinput"; title: string }) {
  const input = useMediaDeviceSelect({ kind });
  const output = useMediaDeviceSelect({ kind: "audiooutput" });
  const pick = (k: MediaDeviceKind, id: string, set: (id: string) => Promise<void>) =>
    void set(id)
      .then(() => {
        const prefs = readDevicePrefs();
        writeDevicePrefs({ ...prefs, ...(k === "audioinput" ? { audioInput: id } : k === "videoinput" ? { videoInput: id } : { audioOutput: id }) });
      })
      .catch(() => toast.error("Couldn't switch device. Try again."));
  const devices = input.devices.filter((d) => d.deviceId);
  const outputs = kind === "audioinput" ? output.devices.filter((d) => d.deviceId) : [];
  if (devices.length === 0) return null;
  return (
    <Menu side="top" align="start" trigger={<IconButton label={`${title} settings`} icon={<ChevronUp />} variant="ghost" size="sm" className="ml-1 rounded-pill" />}>
      <MenuLabel>{title}</MenuLabel>
      {devices.map((d, i) => (
        <MenuChoice key={d.deviceId} checked={input.activeDeviceId === d.deviceId} onSelect={() => pick(kind, d.deviceId, input.setActiveMediaDevice)}>
          {d.label || `${title} ${i + 1}`}
        </MenuChoice>
      ))}
      {outputs.length > 0 && (
        <>
          <MenuSeparator />
          <MenuLabel>Speaker</MenuLabel>
          {outputs.map((d, i) => (
            <MenuChoice key={d.deviceId} checked={output.activeDeviceId === d.deviceId} onSelect={() => pick("audiooutput", d.deviceId, output.setActiveMediaDevice)}>
              {d.label || `Speaker ${i + 1}`}
            </MenuChoice>
          ))}
        </>
      )}
    </Menu>
  );
}

function PanelButton({
  label,
  shortcut,
  icon,
  active,
  badge,
  badgeTone = "primary",
  count,
  onClick,
}: {
  label: string;
  shortcut: string;
  icon: ReactNode;
  active: boolean;
  badge?: number | undefined;
  badgeTone?: "primary" | "warning";
  count?: number;
  onClick: () => void;
}) {
  return (
    <Tooltip content={`${label} · ${shortcut}`}>
      <span className="relative flex">
        <IconButton label={count !== undefined ? `${label} (${count})` : label} icon={icon} variant={active ? "primary" : "secondary"} className={round} aria-pressed={active} onClick={onClick} />
        {badge ? (
          <Badge tone={badgeTone} variant="solid" pop key={badge} className="pointer-events-none absolute -top-1 -right-1 h-5 min-w-5 justify-center px-1">
            {badge > 99 ? "99+" : badge}
          </Badge>
        ) : null}
      </span>
    </Tooltip>
  );
}

/** Six reactions in a row above the bar; Escape closes. */
function ReactionBar({ onPick, onClose }: { onPick: (r: CallsReaction) => void; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    ref.current?.querySelector("button")?.focus();
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div ref={ref} role="toolbar" aria-label="Reactions" className="absolute bottom-full left-1/2 flex -translate-x-1/2 gap-1 rounded-pill border border-border bg-surface-raised p-1 shadow-lg">
      {CALLS_REACTIONS.map((r) => (
        <button
          key={r}
          type="button"
          onClick={() => onPick(r)}
          className="flex size-control-lg items-center justify-center rounded-pill text-title hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
          aria-label={`React ${r}`}
        >
          {r}
        </button>
      ))}
    </div>
  );
}

/** Whether a toggle shortcut matches: ⌘/Ctrl + key, or ⌘/Ctrl + Alt + key. */
export function matchShortcut(e: KeyboardEvent, key: string, alt = false): boolean {
  const mod = e.metaKey || e.ctrlKey;
  return mod && e.altKey === alt && e.code === `Key${key.toUpperCase()}`;
}

