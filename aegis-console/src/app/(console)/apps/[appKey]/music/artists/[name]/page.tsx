"use client";

import { useParams } from "next/navigation";
import { MusicArtistDetailPage } from "@/components/music/music-artist-detail";

/** 歌手详情：统计、每日走势、代表作、相似歌手 */
export default function MusicArtistPage() {
  const params = useParams<{ appKey: string; name: string }>();
  const appKey = decodeURIComponent(String(params?.appKey ?? ""));
  const name = decodeURIComponent(String(params?.name ?? ""));
  return <MusicArtistDetailPage appKey={appKey} name={name} />;
}
