"use client";

import { useState } from "react";
import { RefreshCw } from "lucide-react";
import type { EmailDelivery, EmailProviderMeta } from "@/lib/api/types";
import { useEmailDeliveriesQuery, type EmailScope } from "@/lib/email-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

/**
 * 投递留痕。
 *
 * 状态语义按通道分两档，界面上必须能区分开：
 *   - SMTP 止于 `sent`：协议本身没有回执，那就是这条链路能确认的最终状态；
 *   - 其余八家都会由 webhook 推进到 delivered / bounced / complained。
 *
 * 因此「停在已发送」在前一档是正常的、在后一档才值得怀疑 —— 把这句话写在空态里，
 * 比让人对着一列 sent 猜要好。
 */
const DELIVERY_STATUS: Record<
  string,
  { label: string; variant: "success" | "warning" | "danger" | "info" | "outline" }
> = {
  pending: { label: "已入队", variant: "info" },
  sent: { label: "已发送", variant: "info" },
  delivered: { label: "已送达", variant: "success" },
  bounced: { label: "已退信", variant: "danger" },
  complained: { label: "被投诉", variant: "danger" },
  rejected: { label: "被拒绝", variant: "danger" },
  failed: { label: "发送失败", variant: "danger" }
};

/** 用途 → 中文。留痕里的 purpose 是后端定的英文键，直接显示等于让人猜。 */
const PURPOSE_LABELS: Record<string, string> = {
  test: "通道测试",
  register: "注册验证",
  login: "登录验证",
  reset: "重置验证",
  password_reset: "密码重置",
  welcome: "欢迎信",
  profile_change: "资料变更",
  notification: "通知",
  document: "凭证/附件",
  receipt: "支付凭证"
};

function fmtDate(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "—"
    : date.toLocaleString("zh-CN", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit"
      });
}

export function EmailDeliveryTable({
  scope,
  providers
}: {
  scope: EmailScope;
  providers: EmailProviderMeta[];
}) {
  const [status, setStatus] = useState("all");
  const [provider, setProvider] = useState("all");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  // 300ms 防抖而不是「输入框 + 查询按钮」：节流是机器的活，不该转嫁给操作者。
  const debouncedKeyword = useDebouncedValue(keyword, 300);

  const query = useEmailDeliveriesQuery(scope, {
    status: status === "all" ? undefined : status,
    provider: provider === "all" ? undefined : provider,
    keyword: debouncedKeyword.trim() || undefined,
    page,
    pageSize: 50
  });

  const items: EmailDelivery[] = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const pageSize = query.data?.pageSize ?? 50;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  /** 筛选任何一项变化都回到第 1 页，否则会停在新条件下不存在的页码上 —— 表现为「明明有数据却是空的」。 */
  function resetPage<T>(setter: (value: T) => void) {
    return (value: T) => {
      setter(value);
      setPage(1);
    };
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          className="h-8 w-56 text-sm"
          placeholder="按收件地址或主题筛选"
          value={keyword}
          onChange={(e) => resetPage(setKeyword)(e.target.value)}
        />
        <Select value={status} onValueChange={resetPage(setStatus)}>
          <SelectTrigger className="h-8 w-32 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            {Object.entries(DELIVERY_STATUS).map(([value, meta]) => (
              <SelectItem key={value} value={value}>
                {meta.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={provider} onValueChange={resetPage(setProvider)}>
          <SelectTrigger className="h-8 w-36 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部服务商</SelectItem>
            {providers.map((item) => (
              <SelectItem key={item.provider} value={item.provider}>
                {item.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          type="button"
          size="sm"
          variant="outline"
          className="h-8"
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
        >
          <RefreshCw className={`size-3.5 ${query.isFetching ? "animate-spin" : ""}`} /> 刷新
        </Button>
        <span className="text-xs text-muted-foreground">共 {total} 条</span>
      </div>

      {items.length === 0 ? (
        <EmptyState
          title="暂无投递记录"
          description="发出的每一封信都会在这里留痕；除 SMTP 外的服务商都会把送达 / 退信 / 投诉状态经回执回填进来。"
        />
      ) : (
        <>
          <div className="overflow-x-auto rounded-xl border">
            <table className="w-full text-sm">
              <thead className="bg-muted/40 text-xs text-muted-foreground">
                <tr>
                  <th className="px-3 py-2 text-left font-medium">收件人</th>
                  <th className="px-3 py-2 text-left font-medium">主题</th>
                  <th className="px-3 py-2 text-left font-medium">用途</th>
                  <th className="px-3 py-2 text-left font-medium">通道</th>
                  <th className="px-3 py-2 text-left font-medium">状态</th>
                  <th className="px-3 py-2 text-left font-medium">时间</th>
                </tr>
              </thead>
              <tbody>
                {items.map((item) => {
                  const statusMeta = DELIVERY_STATUS[item.status] ?? {
                    label: item.status,
                    variant: "outline" as const
                  };
                  const providerName =
                    providers.find((p) => p.provider === item.provider)?.name ?? item.provider;
                  return (
                    <tr key={item.id} className="border-t align-top">
                      <td className="px-3 py-2 font-mono text-xs">{item.toAddress}</td>
                      <td className="max-w-[22rem] truncate px-3 py-2" title={item.subject}>
                        {item.subject || "—"}
                      </td>
                      <td className="px-3 py-2 text-xs text-muted-foreground">
                        {item.purpose ? PURPOSE_LABELS[item.purpose] ?? item.purpose : "—"}
                      </td>
                      <td className="px-3 py-2 text-xs text-muted-foreground">
                        <div>{providerName}</div>
                        {item.configName && <div className="text-[10px] opacity-70">{item.configName}</div>}
                      </td>
                      <td className="px-3 py-2">
                        <Badge variant={statusMeta.variant} size="sm">
                          {statusMeta.label}
                        </Badge>
                        {(item.openCount ?? 0) > 0 && (
                          <span className="ml-1 text-[10px] text-muted-foreground">打开 {item.openCount}</span>
                        )}
                        {item.errorMessage && (
                          <div
                            className="mt-1 max-w-[20rem] text-[11px] text-muted-foreground"
                            title={item.errorMessage}
                          >
                            {item.errorMessage}
                          </div>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-xs text-muted-foreground">
                        {fmtDate(item.deliveredAt || item.sentAt || item.createdAt)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>

          {totalPages > 1 && (
            <div className="flex items-center justify-end gap-2 text-xs text-muted-foreground">
              <Button
                type="button"
                size="sm"
                variant="outline"
                className="h-7"
                disabled={page <= 1}
                onClick={() => setPage((value) => Math.max(1, value - 1))}
              >
                上一页
              </Button>
              <span>
                {page} / {totalPages}
              </span>
              <Button
                type="button"
                size="sm"
                variant="outline"
                className="h-7"
                disabled={page >= totalPages}
                onClick={() => setPage((value) => Math.min(totalPages, value + 1))}
              >
                下一页
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
