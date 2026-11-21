"use client";

import { AlertTriangle, FileText, Languages, Star, Type } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Item, ItemContent, ItemDescription, ItemGroup, ItemMedia, ItemTitle } from "@/components/ui/item";
import { OverviewCard } from "@/components/overview/overview-kit";
import type { PaymentReceiptCapability } from "@/lib/api/types";

/**
 * 凭证能力自述。
 *
 * 放在概览里而不是藏进设置页：缺中日韩字体时中文凭证会静默降级成英文，
 * 而这件事只有在用户下载到一份看不懂的 PDF 之后才会被发现。
 */
export function ReceiptCard({ receipt, className }: { receipt: PaymentReceiptCapability; className?: string }) {
  const locales = receipt.locales ?? [];
  const defaultLocale = locales.find((item) => item.tag === receipt.defaultLocale);
  const unavailable = locales.filter((item) => !item.available).length;

  return (
    <OverviewCard
      icon={FileText}
      tone={receipt.supportsCJK ? "emerald" : "amber"}
      title="凭证能力"
      description="订单与流水凭证 PDF"
      className={className}
      action={
        receipt.supportsCJK ? (
          <Badge variant="success" size="sm">
            中日韩可用
          </Badge>
        ) : (
          <Badge variant="warning" size="sm">
            缺中日韩字体
          </Badge>
        )
      }
    >
      <div className="flex min-w-0 flex-col gap-3">
        <ItemGroup className="-mx-2">
          <Item size="sm" className="px-2 py-1.5">
            <ItemMedia variant="icon">
              <Languages />
            </ItemMedia>
            <ItemContent className="min-w-0">
              <ItemTitle className="text-xs">可选语言</ItemTitle>
              <ItemDescription className="text-[11px]">
                {locales.length} 种{unavailable > 0 ? `，${unavailable} 种暂不可用` : ""}
              </ItemDescription>
            </ItemContent>
          </Item>
          <Item size="sm" className="px-2 py-1.5">
            <ItemMedia variant="icon">
              <Star />
            </ItemMedia>
            <ItemContent className="min-w-0">
              <ItemTitle className="text-xs">默认语言</ItemTitle>
              <ItemDescription className="text-[11px]">
                {defaultLocale ? `${defaultLocale.nativeName}（${defaultLocale.tag}）` : receipt.defaultLocale || "en"}
              </ItemDescription>
            </ItemContent>
          </Item>
          <Item size="sm" className="px-2 py-1.5">
            <ItemMedia variant="icon">
              <Type />
            </ItemMedia>
            <ItemContent className="min-w-0">
              <ItemTitle className="text-xs">字体</ItemTitle>
              <ItemDescription className="truncate font-data text-[11px]" title={receipt.fontStatus}>
                {receipt.fontStatus || "未配置"}
              </ItemDescription>
            </ItemContent>
          </Item>
        </ItemGroup>

        {!receipt.supportsCJK ? (
          <Alert className="border-amber-500/30 bg-amber-500/8 text-amber-800 dark:text-amber-300">
            <AlertTriangle />
            <AlertTitle className="text-xs">中日韩凭证会降级为英文</AlertTitle>
            <AlertDescription className="text-[11px] text-amber-800/80 dark:text-amber-300/80">
              <p>
                配置 <code className="font-data">PAYMENT_RECEIPT_FONT_PATH</code> 后恢复
              </p>
            </AlertDescription>
          </Alert>
        ) : null}

        {receipt.fontNotes && receipt.fontNotes.length > 0 ? (
          <ul className="list-disc space-y-1 pl-4 text-[11px] text-muted-foreground">
            {receipt.fontNotes.map((note) => (
              <li key={note}>{note}</li>
            ))}
          </ul>
        ) : null}
      </div>
    </OverviewCard>
  );
}
