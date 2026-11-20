"use client";

import { useRef, useState } from "react";
import { ChevronDown, ChevronRight, Diamond, Pause, Play, Plus, SkipBack, Trash2, Wand2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import type { CosmeticCatalog, Easing, Layer, TrackProperty } from "@/lib/api/cosmetics";
import { cn } from "@/lib/utils";
import {
  EASING_LABEL,
  PRESETS,
  PROPERTY_LABEL,
  PROPERTY_RANGE,
  animatableProperties,
  applyPreset,
  clampDuration,
  patchLayer,
  presetAvailable,
  removeKeyframe,
  setKeyframe,
  sortFrames,
  updateKeyframe,
  valueAt,
  type PresetKey
} from "./design-ops";
import { NumberField, SelectField } from "./designer-fields";
import { LayerIcon } from "./designer-layers";
import { designTime, useDesigner, useDesignerApi, type KeyframeRef } from "./designer-store";

/**
 * 时间轴：每个图层一行（汇总所有关键帧），展开后每条轨道一行。
 * 关键帧菱形可以拖动改时刻（吸附 10 毫秒，Shift 吸附 100 毫秒，靠近播放头时吸到播放头），点选后在顶栏改值与缓动；
 * 双击轨道空白处在那一刻打一帧；拖动标尺移动播放头。
 */

const LABEL_WIDTH = 188;

export function DesignerTimeline({ catalog, readOnly }: { catalog?: CosmeticCatalog | null; readOnly: boolean }) {
  const api = useDesignerApi();
  const design = useDesigner((s) => s.design);
  const selected = useDesigner((s) => s.selected);
  const keyframe = useDesigner((s) => s.keyframe);
  const playing = useDesigner((s) => s.playing);
  const t = useDesigner((s) => designTime(s));
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const laneRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{ ref: KeyframeRef; width: number; left: number } | null>(null);
  const duration = design.duration;
  const maxDuration = catalog?.vocabulary?.limits?.maxDurationMs ?? 20000;
  const maxKeyframes = catalog?.vocabulary?.limits?.maxKeyframes ?? 64;

  const layers = [...design.layers].reverse();
  const selectedLayer = selected ? design.layers.find((layer) => layer.id === selected) : undefined;
  const selectedFrame = keyframe ? findFrame(design.layers, keyframe) : null;

  const timeFromClient = (clientX: number, snap = 10) => {
    const rect = laneRef.current?.getBoundingClientRect();
    if (!rect || !duration) return 0;
    const ratio = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
    return Math.round((ratio * duration) / snap) * snap;
  };

  const scrub = (clientX: number) => {
    const state = api.getState();
    state.setPlaying(false);
    state.setClock(Math.min(duration, timeFromClient(clientX)));
  };

  const addFrame = (layer: Layer, property: TrackProperty, at: number) => {
    const count = layer.tracks?.find((track) => track.property === property)?.keyframes.length ?? 0;
    if (count >= maxKeyframes) return;
    api.getState().update((d) => patchLayer(d, layer.id, (l) => setKeyframe(l, property, at, valueAt(l, property, at))));
  };

  const ensureDuration = () => {
    if (api.getState().design.duration === 0) api.getState().update((d) => ({ ...d, duration: 3000 }));
  };

  const applyPresetKey = (key: PresetKey) => {
    if (!selectedLayer) return;
    ensureDuration();
    const length = api.getState().design.duration || 3000;
    api.getState().update((d) => patchLayer(d, selectedLayer.id, (l) => applyPreset(l, key, length)));
    setExpanded((current) => ({ ...current, [selectedLayer.id]: true }));
  };

  const ticks = tickMarks(duration);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-10 shrink-0 items-center gap-1.5 border-b px-2">
        <Button size="icon-xs" variant="ghost" onClick={() => api.getState().setClock(0)} aria-label="回到开头">
          <SkipBack />
        </Button>
        <Button
          size="icon-xs"
          variant={playing ? "secondary" : "ghost"}
          onClick={() => api.getState().setPlaying(!playing)}
          aria-label={playing ? "暂停" : "播放"}
        >
          {playing ? <Pause /> : <Play />}
        </Button>
        <span className="w-24 text-xs tabular-nums text-muted-foreground">
          {formatMs(t)} / {formatMs(duration)}
        </span>
        <div className="w-36">
          <NumberField
            label="时长"
            suffix="ms"
            value={duration}
            min={0}
            max={maxDuration}
            step={100}
            disabled={readOnly}
            onChange={(value) => api.getState().update((d) => clampDuration(d, Math.round(value)), "design.duration")}
          />
        </div>
        {!readOnly && selectedLayer ? (
          <>
            <span className="mx-1 h-4 w-px bg-border" />
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="xs" variant="outline">
                  <Diamond />
                  打关键帧
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="w-44">
                <DropdownMenuLabel className="text-xs">在 {formatMs(t)} 记录当前值</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {animatableProperties(selectedLayer).map((property) => (
                  <DropdownMenuItem
                    key={property}
                    onSelect={() => {
                      ensureDuration();
                      addFrame(selectedLayer, property, designTime(api.getState()));
                      setExpanded((current) => ({ ...current, [selectedLayer.id]: true }));
                    }}
                  >
                    {PROPERTY_LABEL[property]}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="xs" variant="outline">
                  <Wand2 />
                  动效预设
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="w-40">
                {PRESETS.filter((preset) => presetAvailable(selectedLayer, preset.key)).map((preset) => (
                  <DropdownMenuItem key={preset.key} onSelect={() => applyPresetKey(preset.key)}>
                    {preset.label}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        ) : null}

        {selectedFrame && keyframe ? (
          <div className="ml-auto flex items-center gap-1.5">
            <span className="text-xs font-medium">{PROPERTY_LABEL[keyframe.property]}</span>
            <div className="w-28">
              <NumberField
                label="时刻"
                value={selectedFrame.t}
                min={0}
                max={duration}
                step={10}
                disabled={readOnly}
                onChange={(value) => {
                  api.getState().update(
                    (d) => patchLayer(d, keyframe.layerId, (l) => resortAndTrack(updateKeyframe(l, keyframe.property, keyframe.index, { t: Math.round(value) }), keyframe, api)),
                    `kf.t.${keyframe.layerId}.${keyframe.property}`
                  );
                }}
              />
            </div>
            <div className="w-28">
              <NumberField
                label="值"
                value={selectedFrame.v}
                min={PROPERTY_RANGE[keyframe.property][0]}
                max={PROPERTY_RANGE[keyframe.property][1]}
                step={keyframe.property === "opacity" || keyframe.property.startsWith("scale") ? 0.01 : 1}
                disabled={readOnly}
                onChange={(v) =>
                  api.getState().update(
                    (d) => patchLayer(d, keyframe.layerId, (l) => updateKeyframe(l, keyframe.property, keyframe.index, { v })),
                    `kf.v.${keyframe.layerId}.${keyframe.property}.${keyframe.index}`
                  )
                }
              />
            </div>
            <div className="w-28">
              <SelectField<Easing>
                value={selectedFrame.easing ?? "linear"}
                disabled={readOnly}
                options={(catalog?.vocabulary?.easings ?? (Object.keys(EASING_LABEL) as Easing[])).map((value) => ({
                  value,
                  label: EASING_LABEL[value] ?? value
                }))}
                onChange={(easing) =>
                  api.getState().update((d) => patchLayer(d, keyframe.layerId, (l) => updateKeyframe(l, keyframe.property, keyframe.index, { easing })))
                }
              />
            </div>
            {!readOnly ? (
              <Button
                size="icon-xs"
                variant="ghost"
                onClick={() => {
                  api.getState().update((d) => patchLayer(d, keyframe.layerId, (l) => removeKeyframe(l, keyframe.property, keyframe.index)));
                  api.getState().selectKeyframe(null);
                }}
                aria-label="删除关键帧"
              >
                <Trash2 />
              </Button>
            ) : null}
          </div>
        ) : null}
      </div>

      {duration === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-2 text-xs text-muted-foreground">
          <span>静态设计稿</span>
          {!readOnly ? (
            <Button size="xs" variant="outline" onClick={() => api.getState().update((d) => ({ ...d, duration: 3000 }))}>
              <Plus />
              设为 3 秒循环
            </Button>
          ) : null}
        </div>
      ) : (
        <div className="relative min-h-0 flex-1 overflow-y-auto">
          {/* 标尺 */}
          <div className="sticky top-0 z-10 flex h-6 border-b bg-card">
            <div className="shrink-0 border-r" style={{ width: LABEL_WIDTH }} />
            <div
              ref={laneRef}
              className="relative flex-1 cursor-ew-resize"
              onPointerDown={(event) => {
                event.currentTarget.setPointerCapture(event.pointerId);
                scrub(event.clientX);
              }}
              onPointerMove={(event) => {
                if (event.buttons === 1) scrub(event.clientX);
              }}
            >
              {ticks.map((tick) => (
                <span key={tick} className="absolute top-0 h-full border-l border-border/70" style={{ left: `${(tick / duration) * 100}%` }}>
                  <span className="ml-1 text-[10px] tabular-nums text-muted-foreground">{formatMs(tick)}</span>
                </span>
              ))}
              <Playhead t={t} duration={duration} head />
            </div>
          </div>

          {layers.length === 0 ? <div className="px-3 py-6 text-center text-xs text-muted-foreground">暂无图层</div> : null}
          {layers.map((layer) => {
            const open = expanded[layer.id] ?? selected === layer.id;
            const tracks = layer.tracks ?? [];
            const allFrames = tracks.flatMap((track) => track.keyframes.map((kf) => kf.t));
            return (
              <div key={layer.id} className="border-b last:border-b-0">
                <div className={cn("flex h-7", selected === layer.id && "bg-primary/5")}>
                  <button
                    type="button"
                    className="flex shrink-0 items-center gap-1.5 border-r px-2 text-left text-xs"
                    style={{ width: LABEL_WIDTH }}
                    onClick={() => {
                      api.getState().select(layer.id);
                      setExpanded((current) => ({ ...current, [layer.id]: !open }));
                    }}
                  >
                    {tracks.length ? (
                      open ? <ChevronDown className="size-3 shrink-0" /> : <ChevronRight className="size-3 shrink-0" />
                    ) : (
                      <span className="size-3 shrink-0" />
                    )}
                    <LayerIcon layer={layer} className="size-3.5 shrink-0 text-muted-foreground" />
                    <span className={cn("truncate", layer.hidden && "text-muted-foreground")}>{layer.name || layer.id}</span>
                  </button>
                  <div className="relative flex-1">
                    {Array.from(new Set(allFrames)).map((time) => (
                      <span
                        key={time}
                        className="absolute top-1/2 size-1.5 -translate-x-1/2 -translate-y-1/2 rotate-45 bg-muted-foreground/50"
                        style={{ left: `${(time / duration) * 100}%` }}
                      />
                    ))}
                    <Playhead t={t} duration={duration} />
                  </div>
                </div>
                {open
                  ? tracks.map((track) => (
                      <div key={track.property} className="flex h-7 bg-muted/20">
                        <div className="flex shrink-0 items-center gap-1 border-r pr-1 pl-7 text-[11px] text-muted-foreground" style={{ width: LABEL_WIDTH }}>
                          <span className="flex-1 truncate">{PROPERTY_LABEL[track.property]}</span>
                          {!readOnly ? (
                            <button
                              type="button"
                              title="在播放头处打关键帧"
                              className="inline-flex size-5 items-center justify-center rounded hover:bg-accent hover:text-foreground"
                              onClick={() => addFrame(layer, track.property, designTime(api.getState()))}
                            >
                              <Diamond className="size-3" />
                            </button>
                          ) : null}
                        </div>
                        <div
                          className="relative flex-1"
                          onDoubleClick={(event) => {
                            if (readOnly || !laneRef.current) return;
                            addFrame(layer, track.property, timeFromClient(event.clientX));
                          }}
                        >
                          {track.keyframes.length >= 2 ? <TrackSegments frames={track.keyframes} duration={duration} /> : null}
                          {track.keyframes.map((kf, index) => {
                            const active =
                              keyframe?.layerId === layer.id && keyframe.property === track.property && keyframe.index === index;
                            return (
                              <button
                                key={index}
                                type="button"
                                className={cn(
                                  "absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rotate-45 rounded-[2px] border transition-colors",
                                  active ? "border-amber-600 bg-amber-400" : "border-amber-500/80 bg-card hover:bg-amber-200 dark:hover:bg-amber-900"
                                )}
                                style={{ left: `${(kf.t / duration) * 100}%` }}
                                title={`${formatMs(kf.t)}：${kf.v}（${EASING_LABEL[kf.easing ?? "linear"] ?? kf.easing}）`}
                                onPointerDown={(event) => {
                                  event.stopPropagation();
                                  const ref = { layerId: layer.id, property: track.property, index };
                                  api.getState().selectKeyframe(ref);
                                  if (readOnly || !laneRef.current) return;
                                  const rect = laneRef.current.getBoundingClientRect();
                                  drag.current = { ref, width: rect.width, left: rect.left };
                                  event.currentTarget.setPointerCapture(event.pointerId);
                                  api.getState().begin();
                                }}
                                onPointerMove={(event) => {
                                  const current = drag.current;
                                  if (!current) return;
                                  const playhead = designTime(api.getState());
                                  let next = timeFromClient(event.clientX, event.shiftKey ? 100 : 10);
                                  if (Math.abs(((next - playhead) / duration) * current.width) < 6) next = Math.round(playhead);
                                  api.getState().live((d) =>
                                    patchLayer(d, current.ref.layerId, (l) => updateKeyframe(l, current.ref.property, current.ref.index, { t: next }))
                                  );
                                }}
                                onPointerUp={() => {
                                  const current = drag.current;
                                  drag.current = null;
                                  if (!current) return;
                                  // 松手后按时刻重排，并让选中跟着这一帧走
                                  api.getState().live((d) => patchLayer(d, current.ref.layerId, (l) => resortAndTrack(l, current.ref, api)));
                                  api.getState().end();
                                }}
                              />
                            );
                          })}
                          <Playhead t={t} duration={duration} />
                        </div>
                      </div>
                    ))
                  : null}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function findFrame(layers: Layer[], ref: KeyframeRef) {
  const layer = layers.find((item) => item.id === ref.layerId);
  return layer?.tracks?.find((track) => track.property === ref.property)?.keyframes[ref.index] ?? null;
}

/** 关键帧改了时刻后重新排序，并把选中引用改到它的新下标 */
function resortAndTrack(layer: Layer, ref: KeyframeRef, api: ReturnType<typeof useDesignerApi>): Layer {
  const track = layer.tracks?.find((item) => item.property === ref.property);
  if (!track) return layer;
  const target = track.keyframes[ref.index];
  const sorted = sortFrames(track.keyframes);
  const index = sorted.indexOf(target);
  if (index !== ref.index) queueMicrotask(() => api.getState().selectKeyframe({ ...ref, index }));
  return { ...layer, tracks: layer.tracks!.map((item) => (item.property === ref.property ? { ...item, keyframes: sorted } : item)) };
}

function TrackSegments({ frames, duration }: { frames: Array<{ t: number }>; duration: number }) {
  const sorted = [...frames].sort((a, b) => a.t - b.t);
  const first = sorted[0].t;
  const last = sorted[sorted.length - 1].t;
  return (
    <span
      className="absolute top-1/2 h-[3px] -translate-y-1/2 rounded-full bg-amber-500/25"
      style={{ left: `${(first / duration) * 100}%`, width: `${((last - first) / duration) * 100}%` }}
    />
  );
}

function Playhead({ t, duration, head }: { t: number; duration: number; head?: boolean }) {
  return (
    <span className="pointer-events-none absolute top-0 bottom-0 w-px bg-rose-500" style={{ left: `${(t / Math.max(1, duration)) * 100}%` }}>
      {head ? <span className="absolute -top-0.5 -left-[4px] size-[9px] rotate-45 rounded-[1px] bg-rose-500" /> : null}
    </span>
  );
}

function tickMarks(duration: number) {
  if (!duration) return [];
  const step = duration <= 2000 ? 250 : duration <= 6000 ? 500 : duration <= 12000 ? 1000 : 2000;
  const out: number[] = [];
  for (let t = 0; t < duration; t += step) out.push(t);
  return out;
}

export function formatMs(ms: number) {
  const value = Math.max(0, Math.round(ms));
  return `${(value / 1000).toFixed(value % 1000 === 0 ? 0 : 2)}s`;
}
