"use client";

import { Suspense } from "react";
import { useParams } from "next/navigation";
import { MusicWorkspace } from "@/components/music/music-workspace";
import { LoadingState } from "@/components/ui/data-state";

/** 音乐推荐工作台（docs/music-personalization.md「管理端」）：总览、听众、作品、事件、模型、配置 */
function MusicPageInner() {
  const params = useParams<{ appKey: string }>();
  const appKey = decodeURIComponent(String(params?.appKey ?? ""));
  return <MusicWorkspace appKey={appKey} />;
}

export default function MusicPage() {
  return (
    <Suspense fallback={<LoadingState title="加载音乐推荐" />}>
      <MusicPageInner />
    </Suspense>
  );
}
