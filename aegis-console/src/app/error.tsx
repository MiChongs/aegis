"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { AlertTriangle, ArrowLeft, RotateCcw } from "lucide-react";
import { ErrorPage } from "@/components/error/error-page";

export default function RouteError({
  error,
  reset
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  const router = useRouter();

  useEffect(() => {
    if (typeof window !== "undefined") {
      // eslint-disable-next-line no-console
      console.error("[route-error]", error);
    }
  }, [error]);

  return (
    <ErrorPage
      variant="error"
      title="页面加载失败"
      description="当前页面在渲染过程中出现异常。您可以尝试重新加载；若问题持续，请联系管理员并附上错误 ID。"
      digest={error.digest}
      message={error.message}
      stack={error.stack}
      icon={<AlertTriangle className="size-6" strokeWidth={1.75} />}
      primaryAction={{
        label: "重新加载",
        onClick: () => reset(),
        icon: <RotateCcw className="size-4" />
      }}
      secondaryAction={{
        label: "返回上一页",
        onClick: () => router.back(),
        icon: <ArrowLeft className="size-4" />
      }}
    />
  );
}
