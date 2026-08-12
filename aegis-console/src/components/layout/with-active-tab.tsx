"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";

/**
 * 给组件补上当前 `?tab=`。
 *
 * `useSearchParams()` 必须处在 Suspense 边界内（否则整页退化为客户端渲染并在 build 期报错）。
 * fallback 直接按「无 tab」渲染同一棵树，因此不会有空白闪烁，只是子项高亮晚一帧。
 *
 * 边界要**贴着真正读 tab 的那个组件**，不要上提到整个 Shell ——
 * 那会让主内容区在边界 resolve 时整体重挂载。
 */
export function withActiveTab<P extends { activeTab: string | null }>(Tree: React.ComponentType<P>) {
  type OuterProps = Omit<P, "activeTab">;

  function WithTab(props: OuterProps) {
    const activeTab = useSearchParams().get("tab");
    return <Tree {...(props as P)} activeTab={activeTab} />;
  }

  function Wrapped(props: OuterProps) {
    return (
      <Suspense fallback={<Tree {...(props as P)} activeTab={null} />}>
        <WithTab {...props} />
      </Suspense>
    );
  }

  Wrapped.displayName = `WithActiveTab(${Tree.displayName || Tree.name})`;
  return Wrapped;
}
