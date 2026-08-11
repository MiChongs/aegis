"use client";

import { useState } from "react";
import { appConfig } from "@/lib/env";
import { LegalDialog } from "./legal-dialog";

export function LegalFooter() {
  const [dialog, setDialog] = useState<"terms" | "privacy" | null>(null);

  return (
    <>
      <footer className="relative z-10 flex flex-wrap items-center justify-center gap-x-3 gap-y-1 py-4 text-[11px] text-white/40">
        <span>&copy; {new Date().getFullYear()} {appConfig.platformName}. All rights reserved.</span>
        <span className="hidden sm:inline">|</span>
        <button className="underline underline-offset-2 hover:text-white/70 transition-colors" onClick={() => setDialog("terms")}>
          用户协议 Terms
        </button>
        <span>|</span>
        <button className="underline underline-offset-2 hover:text-white/70 transition-colors" onClick={() => setDialog("privacy")}>
          隐私政策 Privacy
        </button>
      </footer>

      {dialog && (
        <LegalDialog type={dialog} open onOpenChange={(open) => { if (!open) setDialog(null); }} />
      )}
    </>
  );
}
