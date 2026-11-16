"use client";

import { useParams } from "next/navigation";
import { MusicTrackDetailPage } from "@/components/music/music-track-detail";

/** 作品详情：各音源版本、统计、每日曲线、相似作品 */
export default function MusicTrackPage() {
  const params = useParams<{ appKey: string; workKey: string }>();
  const appKey = decodeURIComponent(String(params?.appKey ?? ""));
  const workKey = decodeURIComponent(String(params?.workKey ?? ""));
  return <MusicTrackDetailPage appKey={appKey} workKey={workKey} />;
}
