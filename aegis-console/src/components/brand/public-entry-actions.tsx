"use client";

import Link from "next/link";
import { Button } from "@/components/ui/button";
import { useAuthStore } from "@/lib/auth-store";

type PublicEntryActionsProps = {
  secondaryHref: string;
  secondaryLabel: string;
};

export function PublicEntryActions({ secondaryHref, secondaryLabel }: PublicEntryActionsProps) {
  const hydrated = useAuthStore((state) => state.hydrated);
  const accessToken = useAuthStore((state) => state.accessToken);
  const authenticated = hydrated && Boolean(accessToken);

  return (
    <div className="flex flex-wrap items-center gap-3 max-md:grid max-md:grid-cols-1">
      <Button
        asChild
        size="lg"
        className="min-w-36 rounded-full bg-white px-6 text-[#101114] hover:bg-white hover:text-[#101114] hover:brightness-[0.96] max-md:w-full"
      >
        <Link href={authenticated ? "/overview" : "/login"}>
          {authenticated ? "进入控制台" : "进入管理"}
        </Link>
      </Button>
      <Button
        asChild
        size="lg"
        variant="outline"
        className="min-w-36 rounded-full border-white/[0.14] bg-white/[0.02] px-6 text-white hover:border-white/20 hover:bg-white/[0.06] hover:text-white max-md:w-full"
      >
        <Link href={secondaryHref}>{secondaryLabel}</Link>
      </Button>
    </div>
  );
}
