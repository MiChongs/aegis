"use client";

import type { ComponentType } from "react";
import { Award, CircleUserRound, Crown, Hash, IdCard, Type } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { Tone } from "@/components/users/detail/kit";
import type {
  CosmeticCatalog,
  CosmeticItem,
  CosmeticOwnership,
  CosmeticSlot,
  CosmeticStatus,
  PlateSlot,
  RaritySpec
} from "@/lib/api/cosmetics";
import { cosmeticTermLabel } from "@/lib/cosmetic-names";
import { cn } from "@/lib/utils";

/**
 * 个性装扮的展示口径：槽位、稀有度、状态、来源、持有状态。目录（/cosmetics/catalog）是权威，
 * 这里的常量只是目录还没回来时的兜底，键与 Go 的 cosmetic 包一致。
 */

export const SLOT_ORDER: CosmeticSlot[] = ["avatar_frame", "profile_card", "name_style", "badge", "id_plate", "vip_plate"];

/** 铭牌槽位：设计稿带 plate（文字区域）与 nameStyle（文字效果） */
export function isPlateSlot(slot?: string | null): slot is PlateSlot {
  return slot === "id_plate" || slot === "vip_plate";
}

/** 能在自定义工坊里做的槽位（目录有 workshop 字段时以目录为准；铭牌不进工坊） */
export function workshopSlots(catalog?: CosmeticCatalog | null): CosmeticSlot[] {
  const specs = catalog?.slots;
  if (specs?.length) return SLOT_ORDER.filter((slot) => specs.some((spec) => spec.key === slot && spec.workshop !== false && !spec.plate));
  return SLOT_ORDER.filter((slot) => !isPlateSlot(slot));
}

export const SLOT_META: Record<CosmeticSlot, { label: string; icon: ComponentType<{ className?: string }>; tone: Tone }> = {
  avatar_frame: { label: "头像框", icon: CircleUserRound, tone: "violet" },
  profile_card: { label: "资料卡背景", icon: IdCard, tone: "info" },
  name_style: { label: "昵称样式", icon: Type, tone: "primary" },
  badge: { label: "勋章", icon: Award, tone: "warning" },
  id_plate: { label: "号码铭牌", icon: Hash, tone: "neutral" },
  vip_plate: { label: "会员铭牌", icon: Crown, tone: "danger" }
};

export function slotLabel(slot?: string | null, catalog?: CosmeticCatalog | null) {
  if (!slot) return "";
  return catalog?.slots.find((spec) => spec.key === slot)?.label ?? SLOT_META[slot as CosmeticSlot]?.label ?? slot;
}

const RARITY_FALLBACK: RaritySpec[] = [
  { key: "common", label: "普通", color: "#8A94A6", rank: 1 },
  { key: "rare", label: "稀有", color: "#3B82F6", rank: 2 },
  { key: "epic", label: "史诗", color: "#8B5CF6", rank: 3 },
  { key: "legendary", label: "传说", color: "#F59E0B", rank: 4 },
  { key: "mythic", label: "神话", color: "#EF4444", rank: 5 }
];

export function raritiesOf(catalog?: CosmeticCatalog | null) {
  return catalog?.rarities?.length ? catalog.rarities : RARITY_FALLBACK;
}

export function rarityOf(key?: string | null, catalog?: CosmeticCatalog | null): RaritySpec {
  const list = raritiesOf(catalog);
  return list.find((spec) => spec.key === key) ?? { key: key ?? "", label: key ?? "", color: "#8A94A6", rank: 0 };
}

/** 稀有度徽标：一个标识色圆点 + 名称（标识色来自目录，是数据不是主题色） */
export function RarityBadge({ rarity, catalog, className }: { rarity?: string; catalog?: CosmeticCatalog | null; className?: string }) {
  const spec = rarityOf(rarity, catalog);
  return (
    <Badge variant="outline" size="sm" className={cn("gap-1.5 font-normal", className)}>
      <span className="size-1.5 rounded-full" style={{ backgroundColor: spec.color }} />
      {spec.label}
    </Badge>
  );
}

export const STATUS_META: Record<CosmeticStatus, { label: string; variant: "secondary" | "success" | "outline" }> = {
  draft: { label: "草稿", variant: "secondary" },
  active: { label: "上架", variant: "success" },
  archived: { label: "下架", variant: "outline" }
};

export function StatusBadge({ status }: { status: CosmeticStatus }) {
  const meta = STATUS_META[status] ?? STATUS_META.draft;
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

const SOURCE_FALLBACK: Record<string, string> = {
  admin_grant: "管理员授予",
  points_store: "积分商城",
  lottery: "抽奖",
  card_key: "卡密",
  item_grant: "物品发放",
  invite: "邀请",
  leaderboard: "排行榜",
  promo: "推广活动"
};

export function sourceLabel(source?: string | null, catalog?: CosmeticCatalog | null) {
  if (!source) return "";
  return catalog?.sources.find((spec) => spec.key === source)?.label ?? SOURCE_FALLBACK[source] ?? source;
}

export function unlockLabel(item: Pick<CosmeticItem, "unlock">, catalog?: CosmeticCatalog | null) {
  const mode = item.unlock?.mode ?? "grant";
  const label = catalog?.unlockModes.find((spec) => spec.key === mode)?.label ?? mode;
  if (mode === "level" && item.unlock.minLevel) return `${label} Lv.${item.unlock.minLevel}`;
  if (mode === "vip" && item.unlock.tierIds?.length) {
    const names = item.unlock.tierIds
      .map((id) => catalog?.tiers.find((tier) => tier.id === id)?.name ?? `#${id}`)
      .join("、");
    return `${label}（${names}）`;
  }
  if (mode === "feature" && item.unlock.feature) {
    const name = catalog?.features.find((feature) => feature.tag === item.unlock.feature)?.name ?? item.unlock.feature;
    return `${label}（${name}）`;
  }
  return label;
}

export type OwnershipStateKey = "live" | "ended" | "revoked";

export const OWNERSHIP_STATE_META: Record<OwnershipStateKey, { label: string; variant: "success" | "outline" | "secondary" }> = {
  live: { label: "生效中", variant: "success" },
  ended: { label: "已结束", variant: "outline" },
  revoked: { label: "已收回", variant: "secondary" }
};

export function ownershipState(item: CosmeticOwnership, now: number): OwnershipStateKey {
  if (item.revokedAt) return "revoked";
  if (item.endsAt && new Date(item.endsAt).getTime() <= now) return "ended";
  return "live";
}

export const REASON_LABEL: Record<string, string> = {
  not_owned: "未持有",
  expired: "已过期或被收回",
  need_vip: "需要有效会员",
  need_tier: "需要指定档位的会员",
  need_level: "等级不够",
  need_feature: "没有对应功能",
  unavailable: "装扮已下架或未发布",
  slot_mismatch: "槽位不符",
  need_workshop: "工坊额度已失效"
};

export function reasonLabel(reason?: string | null) {
  if (!reason) return "";
  return REASON_LABEL[reason] ?? reason;
}

/** 工坊零件可用的槽位（昵称样式作品只是一份 nameStyle，用不到零件） */
export const WORKSHOP_PART_SLOTS = ["avatar_frame", "profile_card", "badge"] as const;

/** 工坊作品的状态：只有正常与被运营下架两种 */
export const WORKSHOP_STATUS_META: Record<"active" | "archived", { label: string; variant: "success" | "outline" }> = {
  active: { label: "正常", variant: "success" },
  archived: { label: "已下架", variant: "outline" }
};

export function termText(days: number) {
  return cosmeticTermLabel(days);
}

export function formatBytes(bytes?: number) {
  const value = bytes ?? 0;
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  if (value < 1024 * 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB`;
  return `${(value / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

export function formatCount(value?: number) {
  return (value ?? 0).toLocaleString("zh-CN");
}
