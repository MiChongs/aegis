"use client";

import { Languages } from "lucide-react";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { usePaymentReceiptCapabilityQuery } from "@/lib/commerce-hooks";

/**
 * 凭证语言选择。
 *
 * 放在工具栏而不是每次下载都问一遍：一个人一次会话里通常只用一种语言，
 * 每份凭证都弹一次语言选择是把一个设置项伪装成了一次决策。
 *
 * 缺字体的语言**照常列出但标注出来**，不灰掉 —— 灰掉只会让人以为「不支持中文」，
 * 而事实是「这台机器没装字体，装上就有」。选了会降级成英文，这一点必须说清楚。
 */
export function ReceiptLocaleSelect({
  appId,
  value,
  onChange
}: {
  appId?: number | null;
  value?: string;
  onChange: (locale: string) => void;
}) {
  const query = usePaymentReceiptCapabilityQuery(appId);
  const locales = query.data?.locales ?? [];
  const current = value || query.data?.defaultLocale || "en";
  const selected = locales.find((locale) => locale.tag === current);
  const degraded = selected ? !selected.available : false;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex">
          <Select value={current} onValueChange={onChange} disabled={!appId || locales.length === 0}>
            <SelectTrigger
              className="h-8 w-auto min-w-32 gap-1.5 text-xs"
              data-degraded={degraded}
              aria-label="凭证语言"
            >
              <Languages className="size-3.5 text-muted-foreground" />
              <SelectValue placeholder="凭证语言" />
            </SelectTrigger>
            <SelectContent>
              {locales.map((locale) => (
                <SelectItem key={locale.tag} value={locale.tag} className="text-xs">
                  {locale.nativeName}
                  {locale.available ? null : (
                    <span className="ml-1 text-[10px] text-amber-600">（缺字体，会出英文）</span>
                  )}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </span>
      </TooltipTrigger>
      <TooltipContent side="bottom" className="max-w-64 text-[11px] leading-relaxed">
        下载与寄送凭证时使用的语言。
        {degraded ? "当前环境缺少该语言所需的字体，出具时会降级为英文。" : null}
      </TooltipContent>
    </Tooltip>
  );
}
