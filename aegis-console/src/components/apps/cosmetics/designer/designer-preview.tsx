"use client";

import { useState } from "react";
import type { CosmeticSlot, Design } from "@/lib/api/cosmetics";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { AvatarFrameCanvas, DesignCanvas, NameStyleCanvas } from "../cosmetic-canvas";
import { paintSampleAvatar } from "../renderer";

/**
 * 预览场景：把正在编辑的设计稿放进客户端里会出现的位置，深浅两种背景各看一遍。
 * 头像框套在不同尺寸的头像上、资料卡顶部、排行榜行、勋章挨着昵称、昵称出现在资料卡与排行榜里。
 */

type Tone = "light" | "dark";

const SURFACE: Record<Tone, { bg: string; card: string; text: string; sub: string; line: string }> = {
  light: { bg: "#F4F4F5", card: "#FFFFFF", text: "#18181B", sub: "#71717A", line: "#E4E4E7" },
  dark: { bg: "#09090B", card: "#18181B", text: "#FAFAFA", sub: "#A1A1AA", line: "#27272A" }
};

export function DesignerPreview({ slot, design }: { slot: CosmeticSlot; design: Design }) {
  const [nickname, setNickname] = useState("远航的旅人");
  return (
    <div className="flex flex-col gap-3 p-3">
      <Input value={nickname} maxLength={16} onChange={(event) => setNickname(event.target.value)} className="h-8 text-xs" placeholder="示例昵称" />
      {(["light", "dark"] as Tone[]).map((tone) => (
        <div key={tone} className="flex flex-col gap-3 rounded-xl border p-3" style={{ backgroundColor: SURFACE[tone].bg }}>
          <span className="text-[11px] font-medium" style={{ color: SURFACE[tone].sub }}>
            {tone === "light" ? "浅色" : "深色"}
          </span>
          {slot === "avatar_frame" ? <AvatarScenes design={design} tone={tone} nickname={nickname} /> : null}
          {slot === "profile_card" ? <ProfileScene design={design} tone={tone} nickname={nickname} /> : null}
          {slot === "badge" ? <BadgeScenes design={design} tone={tone} nickname={nickname} /> : null}
          {slot === "name_style" ? <NameScenes design={design} tone={tone} nickname={nickname} /> : null}
        </div>
      ))}
    </div>
  );
}

function SampleAvatar({ size }: { size: number }) {
  // 普通头像（没戴头像框的对照）：用同一个示例头像画法
  return (
    <canvas
      width={size * 2}
      height={size * 2}
      className="shrink-0 rounded-full"
      style={{ width: size, height: size }}
      ref={(el) => {
        const ctx = el?.getContext("2d");
        if (!el || !ctx) return;
        ctx.setTransform(2, 0, 0, 2, 0, 0);
        paintSampleAvatar(ctx, size);
      }}
    />
  );
}

function AvatarScenes({ design, tone, nickname }: { design: Design; tone: Tone; nickname: string }) {
  const s = SURFACE[tone];
  return (
    <>
      <div className="flex items-end justify-around gap-3 py-3">
        {[32, 48, 72, 96].map((size) => (
          <div key={size} className="flex flex-col items-center gap-2">
            <AvatarFrameCanvas design={design} size={size} />
            <span className="text-[10px] tabular-nums" style={{ color: s.sub }}>
              {size}
            </span>
          </div>
        ))}
      </div>
      <div className="flex flex-col items-center gap-2 rounded-lg py-4" style={{ backgroundColor: s.card }}>
        <AvatarFrameCanvas design={design} size={76} />
        <span className="mt-2 text-sm font-semibold" style={{ color: s.text }}>
          {nickname}
        </span>
      </div>
      <LeaderboardRows tone={tone} nickname={nickname} renderAvatar={(size) => <AvatarFrameCanvas design={design} size={size} />} />
    </>
  );
}

function LeaderboardRows({
  tone,
  nickname,
  renderAvatar,
  renderName
}: {
  tone: Tone;
  nickname: string;
  renderAvatar?: (size: number) => React.ReactNode;
  renderName?: (fontSize: number) => React.ReactNode;
}) {
  const s = SURFACE[tone];
  return (
    <div className="overflow-hidden rounded-lg" style={{ backgroundColor: s.card }}>
      {[1, 2, 3].map((rank) => (
        <div key={rank} className="flex items-center gap-3 px-3 py-2.5" style={{ borderTop: rank > 1 ? `1px solid ${s.line}` : undefined }}>
          <span className="w-4 text-center text-xs font-semibold tabular-nums" style={{ color: rank === 2 ? "#F59E0B" : s.sub }}>
            {rank}
          </span>
          {rank === 2 && renderAvatar ? renderAvatar(32) : <SampleAvatar size={32} />}
          <span className="min-w-0 flex-1 truncate text-sm" style={{ color: s.text }}>
            {rank === 2 ? (renderName ? renderName(14) : nickname) : rank === 1 ? "星河漫游" : "晨光"}
          </span>
          <span className="text-xs tabular-nums" style={{ color: s.sub }}>
            {(12_480 - rank * 1_310).toLocaleString("zh-CN")}
          </span>
        </div>
      ))}
    </div>
  );
}

function ProfileScene({ design, tone, nickname }: { design: Design; tone: Tone; nickname: string }) {
  const s = SURFACE[tone];
  const width = 300;
  return (
    <div className="mx-auto flex flex-col overflow-hidden rounded-lg" style={{ backgroundColor: s.card, width }}>
      <div className="relative overflow-hidden" style={{ height: 150 }}>
        {/* 客户端按宽度等比缩放、超出部分裁掉 */}
        <div className="absolute inset-x-0 top-0">
          <DesignCanvas design={design} width={width} />
        </div>
      </div>
      <div className="-mt-7 flex flex-col items-center gap-1.5 pb-4">
        <div className="rounded-full p-0.5" style={{ backgroundColor: s.card }}>
          <SampleAvatar size={56} />
        </div>
        <span className="text-sm font-semibold" style={{ color: s.text }}>
          {nickname}
        </span>
        <span className="text-[11px]" style={{ color: s.sub }}>
          Lv.12
        </span>
      </div>
    </div>
  );
}

function BadgeScenes({ design, tone, nickname }: { design: Design; tone: Tone; nickname: string }) {
  const s = SURFACE[tone];
  return (
    <>
      <div className="flex flex-col gap-2.5 rounded-lg p-3" style={{ backgroundColor: s.card }}>
        {[16, 20, 24].map((size) => (
          <div key={size} className="flex items-center gap-2">
            <SampleAvatar size={size + 12} />
            <span className="font-semibold" style={{ color: s.text, fontSize: size * 0.7 }}>
              {nickname}
            </span>
            <DesignCanvas design={design} width={size} />
            <span className="text-[10px] tabular-nums" style={{ color: s.sub }}>
              {size}
            </span>
          </div>
        ))}
      </div>
      <div className="flex items-center gap-2 rounded-lg p-3" style={{ backgroundColor: s.card }}>
        <span className="text-sm font-semibold" style={{ color: s.text }}>
          {nickname}
        </span>
        {[0, 1, 2].map((index) => (
          <DesignCanvas key={index} design={design} width={20} />
        ))}
      </div>
      <div className="flex justify-center rounded-lg py-4" style={{ backgroundColor: s.card }}>
        <DesignCanvas design={design} width={96} />
      </div>
    </>
  );
}

function NameScenes({ design, tone, nickname }: { design: Design; tone: Tone; nickname: string }) {
  const s = SURFACE[tone];
  return (
    <>
      <div className="flex flex-col items-center gap-2 rounded-lg py-4" style={{ backgroundColor: s.card }}>
        <SampleAvatar size={56} />
        <NameStyleCanvas style={design.nameStyle} text={nickname || "昵称"} fontSize={20} />
      </div>
      <LeaderboardRows tone={tone} nickname={nickname} renderName={(size) => <NameStyleCanvas style={design.nameStyle} text={nickname || "昵称"} fontSize={size} />} />
      <div className={cn("flex items-start gap-2 rounded-lg p-3")} style={{ backgroundColor: s.card }}>
        <SampleAvatar size={28} />
        <div className="flex min-w-0 flex-col gap-1">
          <NameStyleCanvas style={design.nameStyle} text={nickname || "昵称"} fontSize={13} />
          <span className="text-xs" style={{ color: s.text }}>
            这首歌的前奏太好听了
          </span>
        </div>
      </div>
    </>
  );
}

/** 昵称样式槽位的中央舞台：几种背景、几个字号的大号预览 */
export function NameStage({ design, text }: { design: Design; text: string }) {
  const backgrounds = ["#FFFFFF", "#F4F4F5", "#18181B", "#0F172A"];
  return (
    <div className="flex min-h-0 flex-1 items-center justify-center overflow-auto bg-muted/60 p-6">
      <div className="grid w-full max-w-3xl grid-cols-1 gap-3 sm:grid-cols-2">
        {backgrounds.map((bg) => (
          <div key={bg} className="flex flex-col items-center justify-center gap-4 rounded-xl border py-8" style={{ backgroundColor: bg }}>
            <NameStyleCanvas style={design.nameStyle} text={text || "昵称"} fontSize={36} />
            <NameStyleCanvas style={design.nameStyle} text={text || "昵称"} fontSize={16} />
          </div>
        ))}
      </div>
    </div>
  );
}
