"use client";

import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { appConfig } from "@/lib/env";
import { TermsOfServiceContent } from "./terms-of-service";
import { PrivacyPolicyContent } from "./privacy-policy";

type LegalDialogProps = {
  type: "terms" | "privacy";
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

const titles = {
  terms: { zh: "用户服务协议", en: "Terms of Service" },
  privacy: { zh: "隐私政策", en: "Privacy Policy" },
};

export function LegalDialog({ type, open, onOpenChange }: LegalDialogProps) {
  const title = titles[type];
  const name = appConfig.platformName;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl p-0! gap-0! overflow-hidden">
        <DialogHeader className="px-6 pt-6 pb-4 border-b shrink-0">
          <DialogTitle>
            {title.zh}
            <span className="ml-2 text-sm font-normal text-muted-foreground">
              {title.en}
            </span>
          </DialogTitle>
        </DialogHeader>
        <div className="overflow-y-auto max-h-[calc(85vh-5rem)] px-6 py-5">
          {type === "terms" ? <TermsOfServiceContent platformName={name} /> : <PrivacyPolicyContent platformName={name} />}
        </div>
      </DialogContent>
    </Dialog>
  );
}
