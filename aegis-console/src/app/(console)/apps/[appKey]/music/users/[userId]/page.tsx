"use client";

import { useParams } from "next/navigation";
import { MusicUserDetailPage } from "@/components/music/music-user-detail";
import { EmptyState } from "@/components/ui/data-state";

/** 用户听歌画像：画像、实时状态、模型口味、推荐预览、事件时间线 */
export default function MusicUserPage() {
  const params = useParams<{ appKey: string; userId: string }>();
  const appKey = decodeURIComponent(String(params?.appKey ?? ""));
  const userId = Number(params?.userId);
  if (!Number.isInteger(userId) || userId <= 0) return <EmptyState title="用户 ID 无效" />;
  return <MusicUserDetailPage appKey={appKey} userId={userId} />;
}
