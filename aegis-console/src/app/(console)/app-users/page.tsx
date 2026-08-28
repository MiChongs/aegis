"use client";

import { Suspense } from "react";
import { AppUsersPanel } from "@/components/users/app-users/panel";

/**
 * 应用用户 —— 独立的一级页面。
 *
 * 原先塞在 /users 的十一个 Tab 里：一个承载全量用户运营（指标、筛选、批量处置、
 * 导出、逐用户详情）的界面挤在页签里，既没有自己的地址层级（详情页却在
 * /users/app-users/... 下），侧边栏也没法直达。现在列表在 /app-users，
 * 详情在 /app-users/{appKey}/{userId}，一条链路一个层级。
 */
export default function AppUsersPage() {
  // 面板读 URL 查询参数，useSearchParams 必须包在 Suspense 边界内
  return (
    <Suspense>
      <div className="page-stack">
        <AppUsersPanel />
      </div>
    </Suspense>
  );
}
