import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 个性装扮域 API（docs/cosmetics.md）。
 *
 * 类型与 Go 的 `internal/domain/cosmetic`（types.go、catalog.go、design.go、templates.go）一一对应，
 * 字段名即 JSON 标签。设计稿（Design）是三端共用的同一份规格：服务端校验、控制台与客户端各自绘制。
 * 颜色是 `#RRGGBB` 或 `#AARRGGBB`（alpha 在前），浏览器绘制前由渲染器换成 CSS 写法。
 */

export type CosmeticSlot = "avatar_frame" | "profile_card" | "name_style" | "badge" | "id_plate" | "vip_plate";
/** 铭牌槽位：设计稿带 plate（文字区域）与 nameStyle（文字效果） */
export type PlateSlot = "id_plate" | "vip_plate";
export type CosmeticStatus = "draft" | "active" | "archived";
export type CosmeticUnlockMode = "grant" | "free" | "vip" | "level" | "feature";
export type CosmeticOwnershipState = "live" | "ended" | "revoked";

export type LayerType = "image" | "sprite" | "text" | "shape" | "particles";
export type ShapeKind = "rect" | "circle" | "ring" | "star" | "polygon";
export type AvatarShape = "circle" | "rounded" | "square";
export type Placement = "above" | "below";
export type BlendMode = "normal" | "screen" | "multiply" | "add" | "overlay";
export type FitMode = "fill" | "contain" | "cover";
export type Easing = "linear" | "easeIn" | "easeOut" | "easeInOut" | "step" | "backOut" | "elastic";
export type EmitterKind = "point" | "ring" | "rect";
export type ParticleShape = "circle" | "star" | "spark" | "image";
export type NameEffect = "none" | "shimmer" | "pulse" | "cycle" | "glow" | "wave";
export type TextAlign = "left" | "center" | "right";
export type PlateFont = "sans" | "serif" | "mono";
export type TrackProperty =
  | "x"
  | "y"
  | "scale"
  | "scaleX"
  | "scaleY"
  | "rotation"
  | "opacity"
  | "hue"
  | "sweep"
  | "dashOffset";

export type CanvasSize = { width: number; height: number };

export type AvatarSlot = {
  x: number;
  y: number;
  size: number;
  shape: AvatarShape;
  /** rounded 时圆角占边长的比例（0–0.5） */
  radius?: number;
};

export type AssetRef = {
  id: number;
  url?: string;
  width?: number;
  height?: number;
  contentType?: string;
  sha256?: string;
};

export type Keyframe = { t: number; v: number; easing?: Easing };
export type Track = { property: TrackProperty; keyframes: Keyframe[] };

export type Particles = {
  emitter: EmitterKind;
  radius?: number;
  shape: ParticleShape;
  asset?: string;
  colors?: string[];
  count: number;
  lifetime: number;
  speed: number;
  direction: number;
  spread: number;
  outward?: boolean;
  gravity?: number;
  sizeMin: number;
  sizeMax: number;
  spin?: number;
  fade?: boolean;
  shrink?: boolean;
  seed: number;
};

export type Layer = {
  id: string;
  name?: string;
  type: LayerType;
  hidden?: boolean;
  placement?: Placement;
  x: number;
  y: number;
  width: number;
  height: number;
  anchorX: number;
  anchorY: number;
  rotation?: number;
  scale: number;
  opacity: number;
  blend?: BlendMode;

  asset?: string;
  fit?: FitMode;
  tint?: string;
  columns?: number;
  rows?: number;
  frameCount?: number;
  fps?: number;

  text?: string;
  fontSize?: number;
  fontWeight?: number;
  color?: string;
  align?: TextAlign;
  letterSpacing?: number;

  shape?: ShapeKind;
  fill?: string;
  stroke?: string;
  strokeWidth?: number;
  cornerRadius?: number;
  sides?: number;
  innerRatio?: number;
  sweep?: number;
  startAngle?: number;
  dash?: number[];
  dashOffset?: number;
  glow?: number;
  glowColor?: string;

  particles?: Particles;
  tracks?: Track[];
};

export type NameStyle = {
  color: string;
  colors?: string[];
  effect: NameEffect;
  speed?: number;
  fontWeight?: number;
  glowColor?: string;
  glowRadius?: number;
  highlightColor?: string;
};

/**
 * 按字伸缩：画布上 x0–x1 这一段横向可伸缩，两端固定（画布像素，0 ≤ x0 < x1 ≤ canvas.width）。
 * delta = 文字宽 − plate.width（下限 −(x1 − x0) + 1），显示画布宽 = canvas.width + delta。
 */
export type PlateStretch = { x0: number; x1: number };

/**
 * 铭牌的文字区域（docs/cosmetics.md「铭牌」）：文字由客户端填（号码 / 会员有效期），
 * template 里恰好一个 `{value}`；没有 stretch 时一行放不下就等比缩小，不换行、不省略。
 */
export type Plate = {
  x: number;
  y: number;
  width: number;
  height: number;
  template: string;
  align: TextAlign;
  /** 画布像素 */
  fontSize: number;
  /** 100–900 */
  fontWeight: number;
  /** em，-0.2–1 */
  letterSpacing: number;
  font: PlateFont;
  /** 按字伸缩（可选，推荐） */
  stretch?: PlateStretch;
};

export type Design = {
  version: number;
  canvas: CanvasSize;
  avatar?: AvatarSlot;
  duration: number;
  background?: string;
  layers: Layer[];
  nameStyle?: NameStyle;
  /** 只有铭牌槽位 */
  plate?: Plate;
  assets?: Record<string, AssetRef>;
};

export type CosmeticUnlock = {
  mode: CosmeticUnlockMode;
  tierIds?: number[];
  minLevel?: number;
  feature?: string;
  hint?: string;
};

export type CosmeticItem = {
  id: number;
  appid?: number;
  slot: CosmeticSlot;
  code: string;
  name: string;
  description?: string;
  rarity: string;
  series?: string;
  status: CosmeticStatus;
  unlock: CosmeticUnlock;
  /** 已发布的设计稿；列表接口不带 */
  design?: Design | null;
  /** 草稿；只在单件接口返回 */
  draft?: Design | null;
  revision: number;
  draftDirty?: boolean;
  preview?: string;
  sortOrder: number;
  hidden: boolean;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
  publishedAt?: string;
  owners?: number;
  equipped?: number;
  /** official 官方 / workshop 工坊作品 */
  origin?: "official" | "workshop";
  /** 工坊作品的作者 */
  creatorUserId?: number;
  /** 工坊作品被运营下架的原因（作者可见） */
  takedownReason?: string;
};

export type CosmeticOwnership = {
  id: number;
  appid?: number;
  userId?: number;
  cosmeticId: number;
  source: string;
  sourceRef?: string;
  durationDays: number;
  startsAt: string;
  endsAt?: string;
  revokedAt?: string;
  revokedBy?: string;
  revokeReason?: string;
  operator?: string;
  createdAt: string;
  cosmeticName?: string;
  slot?: string;
  rarity?: string;
  userAccount?: string;
  userNickname?: string;
};

/**
 * 自定义工坊设置（docs/cosmetics.md「自定义工坊」）。一个人的额度 =
 * max(freeQuota, 有效会员时的 vipQuota, 所在档位的 tierQuotas)，上限 50；关闭时为 0。
 */
export type WorkshopSettings = {
  enabled: boolean;
  freeQuota: number;
  vipQuota: number;
  /** 会员档位编号（字符串）→ 额度 */
  tierQuotas?: Record<string, number> | null;
};

export type CosmeticSettings = {
  appid?: number;
  enabled: boolean;
  badgeSlots: number;
  workshop?: WorkshopSettings | null;
  updatedBy?: string;
  updatedAt?: string;
};

export type CosmeticAsset = {
  id: number;
  appid?: number;
  name: string;
  kind: "image" | "sprite";
  contentType: string;
  width: number;
  height: number;
  size: number;
  sha256: string;
  url: string;
  uploadedBy?: string;
  createdAt: string;
  usedBy: number;
};

export type CosmeticRevision = {
  id: number;
  cosmeticId: number;
  revision: number;
  design?: Design | null;
  note?: string;
  publishedBy?: string;
  createdAt: string;
};

export type SlotSpec = {
  key: CosmeticSlot;
  label: string;
  hint: string;
  canvas: CanvasSize;
  avatar?: AvatarSlot;
  layered: boolean;
  square: boolean;
  multiple: boolean;
  minCanvas: number;
  maxCanvas: number;
  /** 铭牌槽位（画布最长边 minCanvas–maxCanvas，任意比例） */
  plate?: boolean;
  /** 能否在自定义工坊里做 */
  workshop?: boolean;
};

export type RaritySpec = { key: string; label: string; color: string; rank: number };
export type UnlockSpec = { key: CosmeticUnlockMode; label: string; hint: string };
export type SourceSpec = { key: string; label: string };

export type DesignLimits = {
  maxLayers: number;
  maxTracks: number;
  maxKeyframes: number;
  maxDurationMs: number;
  maxParticles: number;
  maxTextRunes: number;
  maxAssetsPerItem: number;
  maxSpriteFrames: number;
  maxAssetBytes: number;
  maxPreviewBytes: number;
  maxBadgeSlots: number;
  maxGrantDays: number;
  maxPlateTemplateRunes?: number;
};

export type DesignVocabulary = {
  version: number;
  layerTypes: LayerType[];
  shapes: ShapeKind[];
  avatarShapes: AvatarShape[];
  placements: Placement[];
  blends: BlendMode[];
  fits: FitMode[];
  easings: Easing[];
  emitters: EmitterKind[];
  particles: ParticleShape[];
  nameEffects: NameEffect[];
  aligns: TextAlign[];
  plateAligns?: TextAlign[];
  plateFonts?: PlateFont[];
  properties: TrackProperty[];
  limits: DesignLimits;
};

export type CosmeticTemplate = {
  key: string;
  slot: CosmeticSlot;
  name: string;
  description: string;
  rarity: string;
  design: Design;
};

export type CosmeticCatalog = {
  slots: SlotSpec[];
  rarities: RaritySpec[];
  unlockModes: UnlockSpec[];
  sources: SourceSpec[];
  vocabulary: DesignVocabulary;
  templates: CosmeticTemplate[];
  tiers: Array<{ id: number; name: string }>;
  features: Array<{ tag: string; name: string; isActive: boolean }>;
  settings: CosmeticSettings;
  assetTypes: string[];
};

/** 客户端看到的一件装扮（Loadout 里的每一项） */
export type CosmeticView = {
  id: number;
  slot: CosmeticSlot;
  code: string;
  name: string;
  description?: string;
  rarity: string;
  series?: string;
  revision: number;
  preview?: string;
  design?: Design | null;
};

export type CosmeticLoadout = {
  avatarFrame?: CosmeticView;
  profileCard?: CosmeticView;
  nameStyle?: CosmeticView;
  /** 号码铭牌（用户有号码时客户端才显示） */
  idPlate?: CosmeticView;
  /** 会员铭牌（用户有有效会员时客户端才显示） */
  vipPlate?: CosmeticView;
  badges: CosmeticView[];
};

export type CosmeticEquipRow = {
  slot: CosmeticSlot;
  position: number;
  cosmeticId: number;
  name: string;
  equippedAt: string;
  effective: boolean;
  reason: string;
  /** 戴的是用户自己的工坊作品 */
  workshop?: boolean;
};

export type CosmeticUserProfile = {
  ownerships: CosmeticOwnership[];
  equips: CosmeticEquipRow[];
  loadout: CosmeticLoadout;
  facts: { isVip: boolean; tierIds?: number[] | null; level: number; features?: string[] | null };
};

export type CosmeticGrantResult = {
  ownership?: CosmeticOwnership;
  alreadyPermanent?: boolean;
  cosmeticName: string;
  slot: string;
  /** 按 equip 戴上了；position 是勋章戴在第几位 */
  equipped?: boolean;
  position?: number;
};

export type CosmeticStats = {
  items: number;
  liveItems: number;
  owners: number;
  ownerships: number;
  wearers: number;
  bySlot: Array<{ slot: CosmeticSlot; items: number; owners: number; equipped: number }>;
  bySource: Array<{ source: string; count: number }>;
  topItems: Array<{ id: number; name: string; slot: CosmeticSlot; rarity: string; owners: number; equipped: number }>;
  recentGrants: CosmeticOwnership[];
  dailyGrants: Array<{ day: string; count: number }>;
  expiringSoon: number;
  assetCount: number;
  assetBytes: number;
  draftsPending: number;
};

export type CosmeticItemPayload = {
  slot?: CosmeticSlot;
  code: string;
  name: string;
  description: string;
  rarity: string;
  series: string;
  unlock: CosmeticUnlock;
  sortOrder: number;
  hidden: boolean;
  /** 新建时套用的模板键 */
  template?: string;
};

export type CosmeticItemFilter = {
  slot?: string;
  status?: string;
  rarity?: string;
  keyword?: string;
};

export type CosmeticAssetFilter = { kind?: string; keyword?: string; page?: number; limit?: number };

export type CosmeticOwnershipFilter = {
  userId?: number;
  cosmeticId?: number;
  source?: string;
  state?: string;
  page?: number;
  limit?: number;
};

export type Paged<T> = { items: T[]; total: number };

/** 工坊零件：运营从素材库里挑出来、允许用户在作品里使用的图片或序列帧（cosmetic.Part） */
export type WorkshopPartSlot = "avatar_frame" | "profile_card" | "badge";

export type WorkshopPart = {
  id: number;
  appid?: number;
  assetId: number;
  name: string;
  category: string;
  slots: WorkshopPartSlot[];
  /** 序列帧的切分；frameCount 为 0 即图片零件 */
  columns?: number;
  rows?: number;
  frameCount?: number;
  fps?: number;
  sortOrder: number;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
  asset?: CosmeticAsset | null;
};

export type WorkshopPartInput = {
  assetId: number;
  name: string;
  category: string;
  slots: WorkshopPartSlot[];
  columns: number;
  rows: number;
  frameCount: number;
  fps: number;
  sortOrder: number;
  enabled: boolean;
};

/** 管理端看到的工坊作品（cosmetic.WorkshopAdminItem）：一件 origin = workshop 的装扮 + 作者 */
export type WorkshopAdminItem = CosmeticItem & {
  creatorUserId: number;
  creatorAccount?: string;
  creatorNickname?: string;
};

export type WorkshopItemFilter = {
  userId?: number;
  slot?: string;
  status?: string;
  keyword?: string;
  page?: number;
  limit?: number;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/cosmetics`;

// ── 目录与设置 ──

export function getCosmeticCatalog(token: string, appKey: string) {
  return apiRequest<CosmeticCatalog>(`${base(appKey)}/catalog`, { token });
}

export function getCosmeticSettings(token: string, appKey: string) {
  return apiRequest<CosmeticSettings>(`${base(appKey)}/settings`, { token });
}

/** workshop 不传时服务端保持原样 */
export type CosmeticSettingsPayload = { enabled: boolean; badgeSlots: number; workshop?: WorkshopSettings };

export function saveCosmeticSettings(token: string, appKey: string, settings: CosmeticSettingsPayload) {
  return apiRequest<CosmeticSettings>(`${base(appKey)}/settings`, {
    method: "PUT",
    token,
    body: JSON.stringify(settings)
  });
}

// ── 素材 ──

export function listCosmeticAssets(token: string, appKey: string, filter: CosmeticAssetFilter = {}) {
  return apiRequest<Paged<CosmeticAsset>>(`${base(appKey)}/assets${buildQuery(filter)}`, { token });
}

export function uploadCosmeticAsset(
  token: string,
  appKey: string,
  input: { file: File; name?: string; kind: "image" | "sprite" }
) {
  const form = new FormData();
  form.append("file", input.file);
  form.append("kind", input.kind);
  if (input.name) form.append("name", input.name);
  return apiRequest<CosmeticAsset>(`${base(appKey)}/assets`, { method: "POST", token, body: form });
}

export function deleteCosmeticAsset(token: string, appKey: string, assetId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/assets/${assetId}`, { method: "DELETE", token });
}

// ── 装扮 ──

export function listCosmeticItems(token: string, appKey: string, filter: CosmeticItemFilter = {}) {
  return apiRequest<CosmeticItem[]>(`${base(appKey)}/items${buildQuery(filter)}`, { token });
}

export function getCosmeticItem(token: string, appKey: string, itemId: number) {
  return apiRequest<CosmeticItem>(`${base(appKey)}/items/${itemId}`, { token });
}

export function createCosmeticItem(token: string, appKey: string, payload: CosmeticItemPayload) {
  return apiRequest<CosmeticItem>(`${base(appKey)}/items`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateCosmeticItem(token: string, appKey: string, itemId: number, payload: CosmeticItemPayload) {
  return apiRequest<CosmeticItem>(`${base(appKey)}/items/${itemId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteCosmeticItem(token: string, appKey: string, itemId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/items/${itemId}`, { method: "DELETE", token });
}

export function reorderCosmeticItems(token: string, appKey: string, itemIds: number[]) {
  return apiRequest<{ updated: number }>(`${base(appKey)}/item-order`, {
    method: "PUT",
    token,
    body: JSON.stringify({ itemIds })
  });
}

export function saveCosmeticDraft(token: string, appKey: string, itemId: number, design: Design) {
  return apiRequest<CosmeticItem>(`${base(appKey)}/items/${itemId}/draft`, {
    method: "PUT",
    token,
    body: JSON.stringify({ design })
  });
}

export function publishCosmeticItem(token: string, appKey: string, itemId: number, note: string) {
  return apiRequest<CosmeticItem>(`${base(appKey)}/items/${itemId}/publish`, {
    method: "POST",
    token,
    body: JSON.stringify({ note })
  });
}

export function uploadCosmeticPreview(token: string, appKey: string, itemId: number, file: Blob) {
  const form = new FormData();
  form.append("file", file, `preview-${itemId}.png`);
  return apiRequest<{ preview: string }>(`${base(appKey)}/items/${itemId}/preview`, { method: "POST", token, body: form });
}

export function setCosmeticStatus(token: string, appKey: string, itemId: number, status: CosmeticStatus) {
  return apiRequest<CosmeticItem>(`${base(appKey)}/items/${itemId}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify({ status })
  });
}

export function duplicateCosmeticItem(token: string, appKey: string, itemId: number) {
  return apiRequest<CosmeticItem>(`${base(appKey)}/items/${itemId}/duplicate`, { method: "POST", token });
}

export function listCosmeticRevisions(token: string, appKey: string, itemId: number) {
  return apiRequest<CosmeticRevision[]>(`${base(appKey)}/items/${itemId}/revisions`, { token });
}

export function restoreCosmeticRevision(token: string, appKey: string, itemId: number, revision: number) {
  return apiRequest<CosmeticItem>(`${base(appKey)}/items/${itemId}/revisions/${revision}/restore`, {
    method: "POST",
    token
  });
}

// ── 持有记录、统计与单个用户 ──

export function listCosmeticOwnerships(token: string, appKey: string, filter: CosmeticOwnershipFilter = {}) {
  return apiRequest<Paged<CosmeticOwnership>>(`${base(appKey)}/ownerships${buildQuery(filter)}`, { token });
}

export function getCosmeticStats(token: string, appKey: string) {
  return apiRequest<CosmeticStats>(`${base(appKey)}/stats`, { token });
}

export function getUserCosmetics(token: string, appKey: string, userId: number) {
  return apiRequest<CosmeticUserProfile>(`${base(appKey)}/users/${userId}`, { token });
}

export function grantUserCosmetic(token: string, appKey: string, userId: number, cosmeticId: number, days: number, equip = false) {
  return apiRequest<CosmeticGrantResult>(`${base(appKey)}/users/${userId}/grant`, {
    method: "POST",
    token,
    body: JSON.stringify({ cosmeticId, days, equip })
  });
}

export function revokeUserCosmetic(token: string, appKey: string, userId: number, ownershipId: number, reason: string) {
  return apiRequest<CosmeticOwnership>(`${base(appKey)}/users/${userId}/ownerships/${ownershipId}/revoke`, {
    method: "POST",
    token,
    body: JSON.stringify({ reason })
  });
}

export function unequipUserCosmetic(token: string, appKey: string, userId: number, slot: CosmeticSlot, position: number) {
  return apiRequest<{ unequipped: boolean }>(`${base(appKey)}/users/${userId}/unequip`, {
    method: "POST",
    token,
    body: JSON.stringify({ slot, position })
  });
}

// ── 自定义工坊 ──

export function listWorkshopParts(token: string, appKey: string) {
  return apiRequest<{ items: WorkshopPart[] }>(`${base(appKey)}/workshop/parts`, { token });
}

export function createWorkshopPart(token: string, appKey: string, input: WorkshopPartInput) {
  return apiRequest<WorkshopPart>(`${base(appKey)}/workshop/parts`, { method: "POST", token, body: JSON.stringify(input) });
}

export function updateWorkshopPart(token: string, appKey: string, partId: number, input: WorkshopPartInput) {
  return apiRequest<WorkshopPart>(`${base(appKey)}/workshop/parts/${partId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(input)
  });
}

export function deleteWorkshopPart(token: string, appKey: string, partId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/workshop/parts/${partId}`, { method: "DELETE", token });
}

export function listWorkshopItems(token: string, appKey: string, filter: WorkshopItemFilter = {}) {
  return apiRequest<Paged<WorkshopAdminItem>>(`${base(appKey)}/workshop/items${buildQuery(filter)}`, { token });
}

/** 下架作品；reason 必填，作者在工坊里看得到 */
export function takedownWorkshopItem(token: string, appKey: string, itemId: number, reason: string) {
  return apiRequest<CosmeticItem>(`${base(appKey)}/workshop/items/${itemId}/takedown`, {
    method: "POST",
    token,
    body: JSON.stringify({ reason })
  });
}

export function restoreWorkshopItem(token: string, appKey: string, itemId: number) {
  return apiRequest<CosmeticItem>(`${base(appKey)}/workshop/items/${itemId}/restore`, { method: "POST", token });
}
