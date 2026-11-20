"use client";

import { Dices, Trash2, Wand2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import type {
  AvatarShape,
  BlendMode,
  CosmeticCatalog,
  Design,
  EmitterKind,
  FitMode,
  Layer,
  NameEffect,
  NameStyle,
  ParticleShape,
  Particles,
  ShapeKind,
  TextAlign,
  TrackProperty
} from "@/lib/api/cosmetics";
import { trackOf } from "../renderer/timeline";
import {
  AVATAR_ID,
  LAYER_TYPE_LABEL,
  PRESETS,
  PROPERTY_LABEL,
  PROPERTY_RANGE,
  SHAPE_LABEL,
  applyPreset,
  clampDuration,
  editAnimated,
  patchLayer,
  presetAvailable,
  removeTrack,
  setKeyframe,
  valueAt,
  type PresetKey
} from "./design-ops";
import {
  AnchorPicker,
  ColorField,
  ColorListField,
  FieldGroup,
  FieldRow,
  NumberField,
  SelectField,
  SwitchField,
  TextField
} from "./designer-fields";
import { designTime, useDesigner, useDesignerApi } from "./designer-store";

/**
 * 属性检查器：按图层类型列出 docs 字段表里的全部字段。范围取 catalog.vocabulary.limits 与 design.go 的校验规则。
 * 可动画的数值（X、Y、旋转、缩放、不透明度、扫过角度、虚线偏移、横纵缩放、色相）旁有关键帧按钮：
 * 有轨道时改的是「此刻」的关键帧（没有就新建一帧），没有轨道时改的是静态值。
 */

const BLEND_LABEL: Record<BlendMode, string> = { normal: "正常", screen: "滤色", multiply: "正片叠底", add: "加亮", overlay: "叠加" };
const FIT_LABEL: Record<FitMode, string> = { fill: "拉伸填满", contain: "完整显示", cover: "裁切铺满" };
const ALIGN_LABEL: Record<TextAlign, string> = { left: "左对齐", center: "居中", right: "右对齐" };
const EMITTER_LABEL: Record<EmitterKind, string> = { point: "中心点", ring: "圆环", rect: "矩形区域" };
const PARTICLE_LABEL: Record<ParticleShape, string> = { circle: "圆点", star: "星星", spark: "火花", image: "图片" };
const AVATAR_SHAPE_LABEL: Record<AvatarShape, string> = { circle: "圆形", rounded: "圆角方形", square: "方形" };
export const NAME_EFFECT_LABEL: Record<NameEffect, string> = {
  none: "纯色",
  shimmer: "流光",
  pulse: "呼吸",
  cycle: "循环变色",
  glow: "外发光",
  wave: "波浪"
};
const WEIGHTS = [100, 200, 300, 400, 500, 600, 700, 800, 900].map((w) => ({ value: String(w), label: String(w) }));

function opts<T extends string>(values: readonly T[] | undefined, labels: Record<T, string>) {
  return (values ?? (Object.keys(labels) as T[])).map((value) => ({ value, label: labels[value] ?? value }));
}

export function DesignerInspector({
  catalog,
  readOnly,
  onPickParticleAsset
}: {
  catalog?: CosmeticCatalog | null;
  readOnly: boolean;
  onPickParticleAsset: (layerId: string) => void;
}) {
  const slot = useDesigner((s) => s.slot);
  const selected = useDesigner((s) => s.selected);
  const design = useDesigner((s) => s.design);
  const layer = selected && selected !== AVATAR_ID ? design.layers.find((item) => item.id === selected) : undefined;

  if (slot === "name_style") return <NameStyleInspector readOnly={readOnly} />;
  return (
    <div className="flex flex-col">
      {layer ? (
        <LayerInspector key={layer.id} layer={layer} catalog={catalog} readOnly={readOnly} onPickParticleAsset={onPickParticleAsset} />
      ) : (
        <DesignInspector catalog={catalog} readOnly={readOnly} focusAvatar={selected === AVATAR_ID} />
      )}
    </div>
  );
}

function DesignInspector({ catalog, readOnly, focusAvatar }: { catalog?: CosmeticCatalog | null; readOnly: boolean; focusAvatar: boolean }) {
  const api = useDesignerApi();
  const slot = useDesigner((s) => s.slot);
  const design = useDesigner((s) => s.design);
  const spec = catalog?.slots.find((item) => item.key === slot);
  const maxDuration = catalog?.vocabulary?.limits?.maxDurationMs ?? 20000;
  const update = (fn: (d: Design) => Design, key: string) => api.getState().update(fn, `design.${key}`);
  const avatar = design.avatar;

  const setCanvas = (axis: "width" | "height", value: number) =>
    update(
      (d) => ({
        ...d,
        canvas: spec?.square ? { width: value, height: value } : { ...d.canvas, [axis]: value }
      }),
      "canvas"
    );

  return (
    <>
      {avatar ? (
        <FieldGroup title={focusAvatar ? "头像区域（已选中）" : "头像区域"}>
          <div className="grid grid-cols-2 gap-1.5">
            <NumberField label="X" value={avatar.x} min={0} max={design.canvas.width} disabled={readOnly} onChange={(x) => update((d) => ({ ...d, avatar: { ...d.avatar!, x } }), "avatar.x")} />
            <NumberField label="Y" value={avatar.y} min={0} max={design.canvas.height} disabled={readOnly} onChange={(y) => update((d) => ({ ...d, avatar: { ...d.avatar!, y } }), "avatar.y")} />
          </div>
          <NumberField
            label="边长"
            value={avatar.size}
            min={8}
            max={Math.min(design.canvas.width, design.canvas.height)}
            disabled={readOnly}
            onChange={(size) => update((d) => ({ ...d, avatar: { ...d.avatar!, size } }), "avatar.size")}
          />
          <FieldRow label="形状">
            <SelectField
              value={avatar.shape}
              disabled={readOnly}
              options={opts(catalog?.vocabulary?.avatarShapes, AVATAR_SHAPE_LABEL)}
              onChange={(shape) => update((d) => ({ ...d, avatar: { ...d.avatar!, shape, radius: shape === "rounded" ? (d.avatar?.radius ?? 0.2) : undefined } }), "avatar.shape")}
            />
          </FieldRow>
          {avatar.shape === "rounded" ? (
            <NumberField
              label="圆角"
              value={avatar.radius ?? 0}
              min={0}
              max={0.5}
              step={0.01}
              disabled={readOnly}
              onChange={(radius) => update((d) => ({ ...d, avatar: { ...d.avatar!, radius } }), "avatar.radius")}
            />
          ) : null}
          <Button
            size="xs"
            variant="outline"
            disabled={readOnly}
            onClick={() =>
              update((d) => {
                const size = d.avatar!.size;
                return { ...d, avatar: { ...d.avatar!, x: (d.canvas.width - size) / 2, y: (d.canvas.height - size) / 2 } };
              }, "avatar.center")
            }
          >
            居中
          </Button>
        </FieldGroup>
      ) : null}
      <FieldGroup title="画布">
        <div className="grid grid-cols-2 gap-1.5">
          <NumberField
            label="宽"
            value={design.canvas.width}
            min={spec?.minCanvas ?? 16}
            max={spec?.maxCanvas ?? 2048}
            disabled={readOnly}
            onChange={(value) => setCanvas("width", Math.round(value))}
          />
          <NumberField
            label="高"
            value={design.canvas.height}
            min={spec?.minCanvas ?? 16}
            max={spec?.maxCanvas ?? 2048}
            disabled={readOnly || spec?.square}
            onChange={(value) => setCanvas("height", Math.round(value))}
          />
        </div>
        {spec ? (
          <p className="text-[11px] text-muted-foreground">
            边长 {spec.minCanvas}–{spec.maxCanvas}
            {spec.square ? "，必须是正方形" : ""}
          </p>
        ) : null}
        <FieldRow label="底色">
          <ColorField
            value={design.background}
            allowEmpty
            placeholder="透明"
            disabled={readOnly}
            onChange={(background) => update((d) => ({ ...d, background }), "background")}
          />
        </FieldRow>
      </FieldGroup>
      <FieldGroup title="动画">
        <NumberField
          label="时长"
          suffix="毫秒"
          value={design.duration}
          min={0}
          max={maxDuration}
          step={100}
          disabled={readOnly}
          onChange={(value) => update((d) => clampDuration(d, Math.round(value)), "duration")}
        />
        <p className="text-[11px] text-muted-foreground">0 为静态；序列帧与粒子不受时长影响</p>
      </FieldGroup>
      <FieldGroup title="统计">
        <div className="flex flex-wrap gap-1.5">
          <Badge variant="outline" size="sm">
            图层 {design.layers.length}
          </Badge>
          <Badge variant="outline" size="sm">
            素材 {Object.keys(design.assets ?? {}).length}
          </Badge>
          <Badge variant="outline" size="sm">
            轨道 {design.layers.reduce((sum, layer) => sum + (layer.tracks?.length ?? 0), 0)}
          </Badge>
        </div>
      </FieldGroup>
    </>
  );
}

function LayerInspector({
  layer,
  catalog,
  readOnly,
  onPickParticleAsset
}: {
  layer: Layer;
  catalog?: CosmeticCatalog | null;
  readOnly: boolean;
  onPickParticleAsset: (layerId: string) => void;
}) {
  const api = useDesignerApi();
  const design = useDesigner((s) => s.design);
  const t = useDesigner((s) => designTime(s));
  const vocabulary = catalog?.vocabulary;
  const maxSide = Math.max(design.canvas.width, design.canvas.height) * 4;
  const animate = design.duration > 0;

  const up = (fn: (l: Layer) => Layer, key: string) =>
    api.getState().update((d) => patchLayer(d, layer.id, fn), `${layer.id}.${key}`);
  const set = (patch: Partial<Layer>, key: string) => up((l) => ({ ...l, ...patch }), key);

  const ensureDuration = () => {
    if (api.getState().design.duration === 0) api.getState().update((d) => ({ ...d, duration: 3000 }));
  };

  const animated = (property: TrackProperty, extra?: { step?: number; suffix?: string; label?: string; min?: number; max?: number }) => {
    const track = trackOf(layer, property);
    const here = track?.keyframes.some((kf) => Math.abs(kf.t - t) < 1);
    const [min, max] = PROPERTY_RANGE[property];
    return (
      <NumberField
        label={extra?.label ?? PROPERTY_LABEL[property]}
        value={valueAt(layer, property, t)}
        min={extra?.min ?? min}
        max={extra?.max ?? max}
        step={extra?.step ?? 1}
        suffix={extra?.suffix}
        disabled={readOnly || ((property === "scaleX" || property === "scaleY" || property === "hue") && !animate && !track)}
        onChange={(value) => {
          if ((property === "scaleX" || property === "scaleY" || property === "hue") && !animate) return;
          up((l) => editAnimated(l, property, value, t, animate), property);
        }}
        keyframe={
          readOnly
            ? undefined
            : {
                state: here ? "here" : track ? "track" : "none",
                onClick: () => {
                  ensureDuration();
                  const time = designTime(api.getState());
                  up((l) => setKeyframe(l, property, time, valueAt(l, property, time)), `${property}.kf`);
                }
              }
        }
      />
    );
  };

  const applyPresetKey = (key: PresetKey) => {
    ensureDuration();
    const duration = api.getState().design.duration || 3000;
    api.getState().update((d) => patchLayer(d, layer.id, (l) => applyPreset(l, key, duration)));
  };

  const p = layer.particles;
  const setParticles = (patch: Partial<Particles>, key: string) =>
    up((l) => ({ ...l, particles: { ...(l.particles as Particles), ...patch } }), `particles.${key}`);
  const limits = vocabulary?.limits;

  return (
    <>
      <FieldGroup title={`${LAYER_TYPE_LABEL[layer.type]}${layer.type === "shape" && layer.shape ? `：${SHAPE_LABEL[layer.shape]}` : ""}`}>
        <FieldRow label="名称">
          <TextField value={layer.name ?? ""} maxLength={32} disabled={readOnly} onChange={(name) => set({ name }, "name")} />
        </FieldRow>
        <FieldRow label="编号">
          <TextField
            value={layer.id}
            mono
            maxLength={32}
            disabled={readOnly}
            onChange={(id) => {
              const clean = id.replace(/[^A-Za-z0-9_-]/g, "");
              if (!clean || design.layers.some((other) => other.id === clean && other.id !== layer.id)) return;
              api.getState().update((d) => patchLayer(d, layer.id, { id: clean }), `${layer.id}.id`);
              api.getState().select(clean);
            }}
          />
        </FieldRow>
        <FieldRow label="隐藏">
          <SwitchField checked={Boolean(layer.hidden)} disabled={readOnly} onChange={(hidden) => set({ hidden }, "hidden")} />
        </FieldRow>
        {design.avatar ? (
          <FieldRow label="位置">
            <SelectField
              value={layer.placement ?? "above"}
              disabled={readOnly}
              options={[
                { value: "above", label: "头像之上" },
                { value: "below", label: "头像之下" }
              ]}
              onChange={(placement) => set({ placement }, "placement")}
            />
          </FieldRow>
        ) : null}
      </FieldGroup>

      <FieldGroup title="变换">
        <div className="grid grid-cols-2 gap-1.5">
          {animated("x")}
          {animated("y")}
          <NumberField label="宽" value={layer.width} min={1} max={maxSide} disabled={readOnly} onChange={(width) => set({ width }, "width")} />
          <NumberField label="高" value={layer.height} min={1} max={maxSide} disabled={readOnly} onChange={(height) => set({ height }, "height")} />
          {animated("rotation", { suffix: "°" })}
          {animated("scale", { step: 0.01, max: 20 })}
        </div>
        <div className="flex items-center gap-2">
          <AnchorPicker x={layer.anchorX} y={layer.anchorY} disabled={readOnly} onChange={(anchorX, anchorY) => set({ anchorX, anchorY }, "anchor")} />
          <div className="grid flex-1 gap-1.5">
            <NumberField label="锚点 X" value={layer.anchorX} min={0} max={1} step={0.01} disabled={readOnly} onChange={(anchorX) => set({ anchorX }, "anchorX")} />
            <NumberField label="锚点 Y" value={layer.anchorY} min={0} max={1} step={0.01} disabled={readOnly} onChange={(anchorY) => set({ anchorY }, "anchorY")} />
          </div>
        </div>
      </FieldGroup>

      <FieldGroup title="外观">
        {animated("opacity", { step: 0.01 })}
        <FieldRow label="混合">
          <SelectField value={layer.blend ?? "normal"} disabled={readOnly} options={opts(vocabulary?.blends, BLEND_LABEL)} onChange={(blend) => set({ blend }, "blend")} />
        </FieldRow>
        {layer.type === "shape" || layer.type === "text" ? (
          <>
            <NumberField
              label="发光"
              value={layer.glow ?? 0}
              min={0}
              max={200}
              disabled={readOnly}
              onChange={(glow) => up((l) => ({ ...l, glow, glowColor: glow > 0 ? (l.glowColor ?? "#CCFFFFFF") : l.glowColor }), "glow")}
            />
            {(layer.glow ?? 0) > 0 ? (
              <FieldRow label="发光色">
                <ColorField value={layer.glowColor} disabled={readOnly} onChange={(glowColor) => set({ glowColor }, "glowColor")} />
              </FieldRow>
            ) : null}
          </>
        ) : null}
      </FieldGroup>

      {layer.type === "image" || layer.type === "sprite" ? (
        <FieldGroup title={layer.type === "sprite" ? "序列帧" : "图片"}>
          <FieldRow label="素材">
            <SelectField
              value={layer.asset}
              disabled={readOnly}
              options={Object.keys(design.assets ?? {}).map((key) => ({ value: key, label: `${key}（#${design.assets?.[key].id}）` }))}
              onChange={(asset) => set({ asset }, "asset")}
            />
          </FieldRow>
          <FieldRow label="适配">
            <SelectField value={layer.fit ?? "contain"} disabled={readOnly} options={opts(vocabulary?.fits, FIT_LABEL)} onChange={(fit) => set({ fit }, "fit")} />
          </FieldRow>
          <FieldRow label="染色">
            <ColorField value={layer.tint} allowEmpty placeholder="不染色" disabled={readOnly} onChange={(tint) => set({ tint }, "tint")} />
          </FieldRow>
          {layer.type === "sprite" ? (
            <>
              <div className="grid grid-cols-2 gap-1.5">
                <NumberField
                  label="列"
                  value={layer.columns}
                  min={1}
                  max={limits?.maxSpriteFrames ?? 256}
                  disabled={readOnly}
                  onChange={(columns) => up((l) => ({ ...l, columns, frameCount: Math.min(l.frameCount ?? columns * (l.rows ?? 1), columns * (l.rows ?? 1)) || columns * (l.rows ?? 1) }), "columns")}
                />
                <NumberField
                  label="行"
                  value={layer.rows}
                  min={1}
                  max={limits?.maxSpriteFrames ?? 256}
                  disabled={readOnly}
                  onChange={(rows) => up((l) => ({ ...l, rows, frameCount: Math.min(l.frameCount ?? (l.columns ?? 1) * rows, (l.columns ?? 1) * rows) || (l.columns ?? 1) * rows }), "rows")}
                />
                <NumberField
                  label="帧数"
                  value={layer.frameCount}
                  min={1}
                  max={(layer.columns ?? 1) * (layer.rows ?? 1)}
                  disabled={readOnly}
                  onChange={(frameCount) => set({ frameCount }, "frameCount")}
                />
                <NumberField label="帧率" value={layer.fps} min={0.1} max={60} step={0.5} suffix="fps" disabled={readOnly} onChange={(fps) => set({ fps }, "fps")} />
              </div>
            </>
          ) : null}
        </FieldGroup>
      ) : null}

      {layer.type === "text" ? (
        <FieldGroup title="文字">
          <Textarea
            value={layer.text ?? ""}
            maxLength={limits?.maxTextRunes ?? 32}
            rows={2}
            disabled={readOnly}
            className="min-h-0 text-xs"
            onChange={(event) => set({ text: event.target.value.replace(/\n/g, "") }, "text")}
          />
          <div className="grid grid-cols-2 gap-1.5">
            <NumberField label="字号" value={layer.fontSize} min={4} max={512} disabled={readOnly} onChange={(fontSize) => set({ fontSize }, "fontSize")} />
            <NumberField label="字距" value={layer.letterSpacing ?? 0} min={-50} max={200} step={0.5} disabled={readOnly} onChange={(letterSpacing) => set({ letterSpacing }, "letterSpacing")} />
          </div>
          <FieldRow label="字重">
            <SelectField value={String(layer.fontWeight ?? 600)} disabled={readOnly} options={WEIGHTS} onChange={(value) => set({ fontWeight: Number(value) }, "fontWeight")} />
          </FieldRow>
          <FieldRow label="对齐">
            <SelectField value={layer.align ?? "center"} disabled={readOnly} options={opts(vocabulary?.aligns, ALIGN_LABEL)} onChange={(align) => set({ align }, "align")} />
          </FieldRow>
          <FieldRow label="颜色">
            <ColorField value={layer.color} disabled={readOnly} onChange={(color) => set({ color }, "color")} />
          </FieldRow>
          <FieldRow label="描边">
            <ColorField value={layer.stroke} allowEmpty disabled={readOnly} onChange={(stroke) => set({ stroke, strokeWidth: stroke ? layer.strokeWidth || 2 : 0 }, "stroke")} />
          </FieldRow>
          {layer.stroke ? (
            <NumberField label="描边宽" value={layer.strokeWidth ?? 0} min={0} max={256} step={0.5} disabled={readOnly} onChange={(strokeWidth) => set({ strokeWidth }, "strokeWidth")} />
          ) : null}
        </FieldGroup>
      ) : null}

      {layer.type === "shape" ? (
        <FieldGroup title="形状">
          <FieldRow label="形状">
            <SelectField
              value={layer.shape ?? "rect"}
              disabled={readOnly}
              options={opts(vocabulary?.shapes, SHAPE_LABEL)}
              onChange={(shape: ShapeKind) =>
                up(
                  (l) => ({
                    ...l,
                    shape,
                    sides: shape === "star" || shape === "polygon" ? (l.sides ?? (shape === "star" ? 5 : 6)) : undefined,
                    innerRatio: shape === "star" ? (l.innerRatio ?? 0.45) : undefined,
                    sweep: shape === "ring" ? (l.sweep ?? 360) : undefined,
                    stroke: shape === "ring" ? (l.stroke ?? l.fill ?? "#FFFFFF") : l.stroke,
                    strokeWidth: shape === "ring" ? l.strokeWidth || 4 : l.strokeWidth,
                    tracks: shape === "ring" ? l.tracks : l.tracks?.filter((track) => track.property !== "sweep")
                  }),
                  "shape"
                )
              }
            />
          </FieldRow>
          {layer.shape !== "ring" ? (
            <FieldRow label="填充">
              <ColorField value={layer.fill} allowEmpty disabled={readOnly} onChange={(fill) => set({ fill }, "fill")} />
            </FieldRow>
          ) : null}
          <FieldRow label="描边">
            <ColorField
              value={layer.stroke}
              allowEmpty={layer.shape !== "ring"}
              disabled={readOnly}
              onChange={(stroke) => set({ stroke, strokeWidth: stroke ? layer.strokeWidth || 2 : 0 }, "stroke")}
            />
          </FieldRow>
          <NumberField label="描边宽" value={layer.strokeWidth ?? 0} min={0} max={256} step={0.5} disabled={readOnly} onChange={(strokeWidth) => set({ strokeWidth }, "strokeWidth")} />
          {layer.shape === "rect" ? (
            <NumberField label="圆角" value={layer.cornerRadius ?? 0} min={0} max={Math.min(layer.width, layer.height) / 2} disabled={readOnly} onChange={(cornerRadius) => set({ cornerRadius }, "cornerRadius")} />
          ) : null}
          {layer.shape === "star" || layer.shape === "polygon" ? (
            <NumberField label={layer.shape === "star" ? "角数" : "边数"} value={layer.sides} min={3} max={24} disabled={readOnly} onChange={(sides) => set({ sides: Math.round(sides) }, "sides")} />
          ) : null}
          {layer.shape === "star" ? (
            <NumberField label="内径比" value={layer.innerRatio ?? 0.5} min={0.05} max={0.95} step={0.01} disabled={readOnly} onChange={(innerRatio) => set({ innerRatio }, "innerRatio")} />
          ) : null}
          {layer.shape === "ring" ? (
            <>
              {animated("sweep", { suffix: "°" })}
              <NumberField label="起始角" value={layer.startAngle ?? 0} min={-360} max={360} suffix="°" disabled={readOnly} onChange={(startAngle) => set({ startAngle }, "startAngle")} />
            </>
          ) : null}
          <FieldRow label="虚线">
            <TextField
              value={(layer.dash ?? []).join(", ")}
              placeholder="实, 虚, …"
              disabled={readOnly}
              onChange={(value) => {
                const dash = value
                  .split(/[,，\s]+/)
                  .map(Number)
                  .filter((v) => Number.isFinite(v) && v > 0)
                  .slice(0, 8);
                up(
                  (l) => ({
                    ...l,
                    dash: dash.length ? dash : undefined,
                    dashOffset: dash.length ? l.dashOffset : undefined,
                    tracks: dash.length ? l.tracks : l.tracks?.filter((track) => track.property !== "dashOffset")
                  }),
                  "dash"
                );
              }}
            />
          </FieldRow>
          {layer.dash?.length ? animated("dashOffset", { label: "偏移" }) : null}
        </FieldGroup>
      ) : null}

      {layer.type === "particles" && p ? (
        <FieldGroup
          title="粒子"
          actions={
            !readOnly ? (
              <Button size="icon-xs" variant="ghost" title="换一个随机种子" onClick={() => setParticles({ seed: Math.floor(Math.random() * 1_000_000_000) }, "seed")}>
                <Dices />
              </Button>
            ) : null
          }
        >
          <FieldRow label="发射器">
            <SelectField value={p.emitter} disabled={readOnly} options={opts(vocabulary?.emitters, EMITTER_LABEL)} onChange={(emitter) => setParticles({ emitter }, "emitter")} />
          </FieldRow>
          {p.emitter === "ring" ? (
            <>
              <NumberField label="半径" value={p.radius ?? 0} min={0} max={4096} disabled={readOnly} onChange={(radius) => setParticles({ radius }, "radius")} />
              <FieldRow label="沿半径">
                <SwitchField checked={Boolean(p.outward)} disabled={readOnly} onChange={(outward) => setParticles({ outward }, "outward")} />
              </FieldRow>
            </>
          ) : null}
          <FieldRow label="形状">
            <SelectField value={p.shape} disabled={readOnly} options={opts(vocabulary?.particles, PARTICLE_LABEL)} onChange={(shape) => setParticles({ shape }, "shape")} />
          </FieldRow>
          {p.shape === "image" ? (
            <FieldRow label="素材">
              <div className="flex gap-1">
                <div className="min-w-0 flex-1">
                  <SelectField
                    value={p.asset}
                    disabled={readOnly}
                    options={Object.keys(design.assets ?? {}).map((key) => ({ value: key, label: key }))}
                    onChange={(asset) => setParticles({ asset }, "asset")}
                  />
                </div>
                <Button size="xs" variant="outline" disabled={readOnly} onClick={() => onPickParticleAsset(layer.id)}>
                  选择
                </Button>
              </div>
            </FieldRow>
          ) : null}
          <div className="grid grid-cols-2 gap-1.5">
            <NumberField label="数量" value={p.count} min={1} max={limits?.maxParticles ?? 240} disabled={readOnly} onChange={(count) => setParticles({ count: Math.round(count) }, "count")} />
            <NumberField label="寿命" value={p.lifetime} min={100} max={20000} step={50} suffix="ms" disabled={readOnly} onChange={(lifetime) => setParticles({ lifetime: Math.round(lifetime) }, "lifetime")} />
            <NumberField label="速度" value={p.speed} min={0} max={5000} disabled={readOnly} onChange={(speed) => setParticles({ speed }, "speed")} />
            <NumberField label="重力" value={p.gravity ?? 0} min={-5000} max={5000} disabled={readOnly} onChange={(gravity) => setParticles({ gravity }, "gravity")} />
            <NumberField label="方向" value={p.direction} min={-360} max={360} suffix="°" disabled={readOnly || (p.emitter === "ring" && p.outward)} onChange={(direction) => setParticles({ direction }, "direction")} />
            <NumberField label="散射" value={p.spread} min={0} max={360} suffix="°" disabled={readOnly || (p.emitter === "ring" && p.outward)} onChange={(spread) => setParticles({ spread }, "spread")} />
            <NumberField label="最小" value={p.sizeMin} min={0.1} max={512} step={0.5} disabled={readOnly} onChange={(sizeMin) => setParticles({ sizeMin, sizeMax: Math.max(sizeMin, p.sizeMax) }, "sizeMin")} />
            <NumberField label="最大" value={p.sizeMax} min={p.sizeMin} max={512} step={0.5} disabled={readOnly} onChange={(sizeMax) => setParticles({ sizeMax }, "sizeMax")} />
            <NumberField label="自转" value={p.spin ?? 0} min={-3600} max={3600} suffix="°/s" disabled={readOnly} onChange={(spin) => setParticles({ spin }, "spin")} />
            <NumberField label="种子" value={p.seed} min={0} max={2147483647} disabled={readOnly} onChange={(seed) => setParticles({ seed: Math.round(seed) }, "seed")} />
          </div>
          <div className="grid grid-cols-2 gap-1.5">
            <FieldRow label="淡出">
              <SwitchField checked={Boolean(p.fade)} disabled={readOnly} onChange={(fade) => setParticles({ fade }, "fade")} />
            </FieldRow>
            <FieldRow label="缩小">
              <SwitchField checked={Boolean(p.shrink)} disabled={readOnly} onChange={(shrink) => setParticles({ shrink }, "shrink")} />
            </FieldRow>
          </div>
          {p.shape !== "image" ? (
            <FieldRow label="颜色" className="items-start">
              <ColorListField values={p.colors ?? []} max={8} disabled={readOnly} onChange={(colors) => setParticles({ colors }, "colors")} />
            </FieldRow>
          ) : null}
        </FieldGroup>
      ) : null}

      <FieldGroup title="动画">
        <div className="grid grid-cols-2 gap-1.5">
          {animated("scaleX", { step: 0.01, label: "横缩" })}
          {animated("scaleY", { step: 0.01, label: "纵缩" })}
          {animated("hue", { suffix: "°" })}
        </div>
        {layer.tracks?.length ? (
          <div className="flex flex-col gap-1">
            {layer.tracks.map((track) => (
              <div key={track.property} className="flex items-center gap-2 rounded-md bg-muted/50 px-2 py-1 text-xs">
                <span className="font-medium">{PROPERTY_LABEL[track.property]}</span>
                <span className="text-muted-foreground tabular-nums">{track.keyframes.length} 帧</span>
                {track.keyframes.length < 2 ? (
                  <Badge variant="warning" size="sm" className="h-4 px-1 text-[10px]">
                    至少 2 帧
                  </Badge>
                ) : null}
                {!readOnly ? (
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    className="ml-auto"
                    onClick={() => up((l) => removeTrack(l, track.property), `${track.property}.remove`)}
                    aria-label="删除轨道"
                  >
                    <Trash2 />
                  </Button>
                ) : null}
              </div>
            ))}
          </div>
        ) : null}
        {!readOnly ? (
          <div className="flex flex-wrap gap-1">
            {PRESETS.filter((preset) => presetAvailable(layer, preset.key)).map((preset) => (
              <Button key={preset.key} size="xs" variant="outline" onClick={() => applyPresetKey(preset.key)}>
                <Wand2 />
                {preset.label}
              </Button>
            ))}
          </div>
        ) : null}
      </FieldGroup>
    </>
  );
}

function NameStyleInspector({ readOnly }: { readOnly: boolean }) {
  const api = useDesignerApi();
  const style = useDesigner((s) => s.design.nameStyle) ?? { color: "#3B82F6", effect: "none" as NameEffect, speed: 2400, fontWeight: 600 };
  const set = (patch: Partial<NameStyle>, key: string) =>
    api.getState().update((d) => ({ ...d, nameStyle: { ...(d.nameStyle ?? style), ...patch } }), `name.${key}`);
  const effect = style.effect;
  return (
    <>
      <FieldGroup title="昵称样式">
        <FieldRow label="效果">
          <SelectField
            value={effect}
            disabled={readOnly}
            options={(Object.keys(NAME_EFFECT_LABEL) as NameEffect[]).map((value) => ({ value, label: NAME_EFFECT_LABEL[value] }))}
            onChange={(next) =>
              set(
                {
                  effect: next,
                  colors: next === "cycle" ? (style.colors?.length ? style.colors : [style.color, "#10B981", "#F59E0B"]) : undefined,
                  glowColor: next === "glow" ? (style.glowColor ?? "#80FFFFFF") : style.glowColor,
                  glowRadius: next === "glow" ? (style.glowRadius ?? 8) : style.glowRadius
                },
                "effect"
              )
            }
          />
        </FieldRow>
        <FieldRow label="主色">
          <ColorField value={style.color} disabled={readOnly} onChange={(color) => color && set({ color }, "color")} />
        </FieldRow>
        <FieldRow label="字重">
          <SelectField value={String(style.fontWeight ?? 600)} disabled={readOnly} options={WEIGHTS} onChange={(value) => set({ fontWeight: Number(value) }, "fontWeight")} />
        </FieldRow>
        <NumberField label="周期" value={style.speed ?? 2400} min={300} max={20000} step={100} suffix="毫秒" disabled={readOnly || effect === "none" || effect === "glow"} onChange={(speed) => set({ speed: Math.round(speed) }, "speed")} />
      </FieldGroup>
      {effect === "cycle" ? (
        <FieldGroup title="循环颜色">
          <ColorListField values={style.colors ?? []} max={6} disabled={readOnly} onChange={(colors) => set({ colors }, "colors")} />
        </FieldGroup>
      ) : null}
      {effect === "glow" ? (
        <FieldGroup title="外发光">
          <FieldRow label="颜色">
            <ColorField value={style.glowColor} disabled={readOnly} onChange={(glowColor) => set({ glowColor }, "glowColor")} />
          </FieldRow>
          <NumberField label="半径" value={style.glowRadius ?? 0} min={0} max={32} step={0.5} disabled={readOnly} onChange={(glowRadius) => set({ glowRadius }, "glowRadius")} />
        </FieldGroup>
      ) : null}
      {effect === "shimmer" ? (
        <FieldGroup title="高光">
          <FieldRow label="颜色">
            <ColorField value={style.highlightColor} allowEmpty placeholder="白色 70%" disabled={readOnly} onChange={(highlightColor) => set({ highlightColor }, "highlightColor")} />
          </FieldRow>
        </FieldGroup>
      ) : null}
    </>
  );
}
