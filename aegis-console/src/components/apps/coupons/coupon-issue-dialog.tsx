"use client";

import { useState } from "react";
import { CheckCircle2, Loader2, Send, X, XCircle } from "lucide-react";
import { toast } from "sonner";
import { SwitchRow } from "@/components/apps/app-config-primitives";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api-client";
import type { CouponTemplate, IssueOutcome } from "@/lib/api/coupons";
import { useIssueCouponsMutation } from "@/lib/coupon-hooks";
import { CouponTicket } from "./coupon-shared";
import { Field } from "./coupon-template-dialog";

const MAX_USERS = 500;

/** 从粘贴的文本里取用户 ID：逗号、空白、换行分隔，去重 */
function parseIds(text: string) {
  const out: number[] = [];
  for (const part of text.split(/[\s,，;；]+/)) {
    const id = Number(part.trim());
    if (Number.isInteger(id) && id > 0 && !out.includes(id)) out.push(id);
  }
  return out;
}

/**
 * 给一批用户发券。主路径是搜索选人（不要求管理员记 ID）；批量时可粘贴一串管理端用户 ID。
 * 暂停中的券也能发；每人一个事务，结果逐人列出（库存不足、用户不存在的会单独标出来）。
 */
export function CouponIssueDialog({
  appKey,
  template,
  open,
  onOpenChange
}: {
  appKey: string;
  template: CouponTemplate;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [picked, setPicked] = useState<PickedUser[]>([]);
  const [pasted, setPasted] = useState("");
  const [count, setCount] = useState("1");
  const [notify, setNotify] = useState(true);
  const [results, setResults] = useState<IssueOutcome[] | null>(null);
  const issue = useIssueCouponsMutation(appKey);

  const ids = [...new Set([...picked.map((user) => user.id), ...parseIds(pasted)])];
  const perUser = Math.max(1, Math.min(20, Number(count) || 1));
  const remaining = template.totalQuantity > 0 ? template.totalQuantity - template.issuedCount : null;
  const needed = ids.length * perUser;
  const short = remaining !== null && needed > remaining;
  const nameOf = (id: number) => {
    const user = picked.find((item) => item.id === id);
    return user ? user.nickname || user.account || `UID-${id}` : `UID-${id}`;
  };

  const submit = async () => {
    if (ids.length === 0) return toast.error("请先选择用户");
    if (ids.length > MAX_USERS) return toast.error(`一次最多发给 ${MAX_USERS} 人`);
    try {
      const result = await issue.mutateAsync({ id: template.id, userIds: ids, count: perUser, notify });
      setResults(result.items);
      if (result.succeeded === result.items.length) toast.success(`已发给 ${result.succeeded} 人`);
      else toast.warning(`成功 ${result.succeeded} 人，失败 ${result.items.length - result.succeeded} 人`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "发放失败");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>发券</DialogTitle>
          <DialogDescription>券直接进用户的券包，不看领券中心的人群与限领；会扣发放总量。</DialogDescription>
        </DialogHeader>
        <CouponTicket template={template} size="sm" />

        {results ? (
          <div className="max-h-72 space-y-1 overflow-y-auto rounded-lg border p-2">
            {results.map((item) => (
              <div key={item.userId} className="flex items-center gap-2 rounded px-2 py-1 text-xs">
                {item.ok ? <CheckCircle2 className="size-3.5 text-emerald-600" /> : <XCircle className="size-3.5 text-rose-600" />}
                <span className="flex-1 truncate">{nameOf(item.userId)}</span>
                <span className="truncate text-muted-foreground">{item.ok ? `${item.couponIds?.length ?? 0} 张` : item.message}</span>
              </div>
            ))}
          </div>
        ) : (
          <div className="space-y-4">
            <Field label="选择用户" hint={`已选 ${ids.length} 人`}>
              <UserPicker
                appKey={appKey}
                value={null}
                onChange={(user) => user && !picked.some((item) => item.id === user.id) && setPicked([...picked, user])}
                placeholder="搜索账号 / 昵称 / 邮箱后添加"
              />
              {picked.length > 0 ? (
                <div className="flex flex-wrap gap-1.5">
                  {picked.map((user) => (
                    <span key={user.id} className="flex items-center gap-1 rounded-full border bg-muted/40 py-0.5 pr-1 pl-2.5 text-[11px]">
                      {user.nickname || user.account || `UID-${user.id}`}
                      <button
                        type="button"
                        className="rounded-full p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
                        aria-label="移除"
                        onClick={() => setPicked(picked.filter((item) => item.id !== user.id))}
                      >
                        <X className="size-3" />
                      </button>
                    </span>
                  ))}
                </div>
              ) : null}
            </Field>
            <Field label="批量粘贴" hint="管理端用户 ID，逗号或换行分隔">
              <Textarea rows={2} value={pasted} placeholder="例如：1024, 2048" onChange={(e) => setPasted(e.target.value)} />
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="每人张数" hint="1–20">
                <Input type="number" min={1} max={20} value={count} onChange={(e) => setCount(e.target.value)} />
              </Field>
              <Field label="合计" hint={remaining === null ? "库存不限" : `剩余 ${remaining} 张`}>
                <div className={short ? "pt-2 text-sm font-medium text-rose-600" : "pt-2 text-sm font-medium"}>{needed} 张</div>
              </Field>
            </div>
            <SwitchRow label="发站内信" hint="告诉用户收到了券，点开直达券包" checked={notify} onChange={setNotify} />
          </div>
        )}

        <DialogFooter>
          {results ? (
            <>
              <Button variant="outline" onClick={() => setResults(null)}>
                继续发放
              </Button>
              <Button onClick={() => onOpenChange(false)}>完成</Button>
            </>
          ) : (
            <>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                取消
              </Button>
              <Button onClick={() => void submit()} disabled={issue.isPending || ids.length === 0 || short}>
                {issue.isPending ? <Loader2 className="animate-spin" /> : <Send />}
                发给 {ids.length} 人
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
