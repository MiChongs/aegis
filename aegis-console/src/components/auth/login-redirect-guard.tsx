"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";

export function LoginRedirectGuard() {
  const router = useRouter();
  const hydrated = useAuthStore((state) => state.hydrated);
  const accessToken = useAuthStore((state) => state.accessToken);

  useEffect(() => {
    if (!hydrated || !accessToken) {
      return;
    }
    router.replace("/overview");
  }, [accessToken, hydrated, router]);

  return null;
}
