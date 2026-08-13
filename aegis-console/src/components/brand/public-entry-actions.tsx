"use client";

import Link from "next/link";
import { ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useAuthStore } from "@/lib/auth-store";

type PublicEntryActionsProps = {
  secondaryHref: string;
  secondaryLabel: string;
};

/** 公开页面上的一对入口：主操作进控制台，次操作由调用方指定 */
export function PublicEntryActions({ secondaryHref, secondaryLabel }: PublicEntryActionsProps) {
  const hydrated = useAuthStore((state) => state.hydrated);
  const accessToken = useAuthStore((state) => state.accessToken);
  const authenticated = hydrated && Boolean(accessToken);

  return (
    <div className="flex flex-wrap gap-3 max-sm:flex-col">
      <Button asChild size="lg">
        <Link href={authenticated ? "/overview" : "/login"}>
          {authenticated ? "进入控制台" : "登录控制台"}
          <ArrowRight />
        </Link>
      </Button>
      <Button asChild size="lg" variant="outline">
        <Link href={secondaryHref}>{secondaryLabel}</Link>
      </Button>
    </div>
  );
}
