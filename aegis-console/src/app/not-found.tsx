"use client";

import { useRouter } from "next/navigation";
import Link from "next/link";
import { ArrowLeft, Compass, Home } from "lucide-react";
import { ErrorPage } from "@/components/error/error-page";

export default function NotFound() {
  const router = useRouter();

  return (
    <ErrorPage
      variant="notfound"
      title="找不到此页面"
      description="您访问的路径可能已被移除、重命名，或暂不可用。请检查链接或返回控制台首页。"
      icon={<Compass className="size-6" strokeWidth={1.75} />}
      primaryAction={{
        label: "返回首页",
        onClick: () => router.push("/"),
        icon: <Home className="size-4" />
      }}
      secondaryAction={{
        label: "返回上一页",
        onClick: () => router.back(),
        icon: <ArrowLeft className="size-4" />
      }}
    />
  );
}
