"use client";

import { Suspense } from "react";
import { useParams, useSearchParams } from "next/navigation";
import { AppUserDetailPage } from "@/components/users/detail/app-user-detail-page";

function AppUserDetailRouteInner() {
  const params = useParams<{ appKey: string; userId: string }>();
  const searchParams = useSearchParams();

  const appKey = params.appKey;
  const userId = Number(params.userId);
  const fromHref = searchParams.get("from") || undefined;

  if (!appKey || !Number.isFinite(userId)) {
    return null;
  }

  return <AppUserDetailPage appKey={appKey} userId={userId} fromHref={fromHref} />;
}

export default function AppUserDetailRoutePage() {
  // 详情页读 `?tab=` 与 `?from=`，useSearchParams 必须包在 Suspense 边界内，
  // 否则 next build 会报错并把整页退化为客户端渲染。
  return (
    <Suspense>
      <AppUserDetailRouteInner />
    </Suspense>
  );
}
