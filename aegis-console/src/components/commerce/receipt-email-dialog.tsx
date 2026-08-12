"use client";

import { useState } from "react";
import { Loader2, Mail, UserRound } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

/** 一个可一键填入的收件地址 */
export type RecipientSuggestion = {
  email: string;
  /** 这个地址是谁的（「下单用户」「我自己」…），让人不必核对字符串就知道选的是谁 */
  label: string;
};

/**
 * 管理端凭证寄送弹窗（订单与钱包流水共用）。
 *
 * 收件地址给出候选一键填入 —— 九成场景就是「寄回给下单用户」或「寄给我自己」，
 * 让人把一串邮箱手抄一遍既慢又容易抄错，而寄错地址是把交易明细发给了别人。
 * 仍保留自由输入：财务邮箱、外部审计邮箱这类没法预知。
 *
 * 管理端可以指定任意收件地址，这条路径在后端走审计中间件。
 * 用户自助那条则恒为账号绑定邮箱 —— 允许任意填写等于把平台变成一个
 * 能带 PDF 附件的转发器。
 */
export function ReceiptEmailDialog({
  open,
  onOpenChange,
  title,
  subject,
  reference,
  suggestions = [],
  localeLabel,
  pending,
  onSubmit
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  /** 这份凭证对应的业务说明，用于让操作者确认没发错单 */
  subject: string;
  /** 订单号或流水号 */
  reference: string;
  suggestions?: RecipientSuggestion[];
  /** 将以哪种语言出具，展示用 */
  localeLabel?: string;
  pending: boolean;
  onSubmit: (to: string) => Promise<void>;
}) {
  // 草稿按凭证主体绑定作用域：换一单就自动清空。
  // 上一次填的财务邮箱留在框里，很容易把下一单也误发过去。
  const [draft, setDraft] = useState<{ scope: string; value: string } | null>(null);
  const to = draft?.scope === reference ? draft.value : "";
  const setTo = (value: string) => setDraft({ scope: reference, value });

  const valid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(to.trim());

  async function handleSubmit() {
    if (!valid) {
      toast.error("请填写有效的收件邮箱");
      return;
    }
    try {
      await onSubmit(to.trim());
      toast.success("凭证已寄出", { description: to.trim() });
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "寄送失败");
    }
  }

  const usable = suggestions.filter((item) => item.email.trim());

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            <Mail className="size-4" />
            {title}
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-1 rounded-lg bg-muted p-3 text-xs">
            <div className="font-mono">{reference}</div>
            {subject ? <div className="text-muted-foreground">{subject}</div> : null}
            {localeLabel ? (
              <div className="text-[11px] text-muted-foreground">凭证语言：{localeLabel}</div>
            ) : null}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="receipt-email-to" className="text-xs">
              收件邮箱
            </Label>
            {usable.length > 0 ? (
              <div className="flex flex-wrap gap-1.5">
                {usable.map((item) => (
                  <Badge
                    key={item.email}
                    variant={to.trim() === item.email ? "default" : "outline"}
                    className="cursor-pointer gap-1 text-[11px] font-normal"
                    onClick={() => setTo(item.email)}
                    title={item.email}
                  >
                    <UserRound className="size-3" />
                    {item.label}
                    <span className="max-w-40 truncate opacity-70">{item.email}</span>
                  </Badge>
                ))}
              </div>
            ) : null}
            <Input
              id="receipt-email-to"
              className="h-8 text-xs"
              placeholder={usable.length > 0 ? "或填写其它地址" : "finance@example.com"}
              value={to}
              onChange={(event) => setTo(event.target.value)}
              onKeyDown={(event) => event.key === "Enter" && valid && void handleSubmit()}
            />
            <p className="text-[11px] leading-relaxed text-muted-foreground">
              邮件里同时带 PDF 附件与一条签名下载链接。通道不支持附件时只发链接，
              不会静默把附件丢掉再把信发出去。
            </p>
          </div>

          <div className="flex justify-end gap-2">
            <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button size="sm" className="h-8 text-xs" disabled={!valid || pending} onClick={() => void handleSubmit()}>
              {pending ? <Loader2 className="size-3.5 animate-spin" /> : <Mail className="size-3.5" />}
              {pending ? "寄送中..." : "寄送"}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
