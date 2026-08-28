"use client";

import { Suspense, useEffect } from "react";
import { useParams, useRouter, useSearchParams } from "next/navigation";

/**
 * 旧地址 /users/app-users/{appKey}/{userId} → /app-users/{appKey}/{userId}。
 *
 * 应用用户已从 /users 的页签独立成一级页面。这条重定向保住所有存量入口：
 * 浏览器书签、聊天里分享过的链接、审计记录里的跳转地址。
 */
function LegacyAppUserDetailRedirectInner() {
  const params = useParams<{ appKey: string; userId: string }>();
  const searchParams = useSearchParams();
  const router = useRouter();

  useEffect(() => {
    if (!params.appKey || !params.userId) return;
    const query = searchParams.toString();
    router.replace(
      `/app-users/${params.appKey}/${params.userId}${query ? `?${query}` : ""}`
    );
  }, [params.appKey, params.userId, router, searchParams]);

  return null;
}

export default function LegacyAppUserDetailRedirect() {
  return (
    <Suspense>
      <LegacyAppUserDetailRedirectInner />
    </Suspense>
  );
}
