"use client";

import { useState } from "react";
import { toast } from "sonner";
import { ShieldAlert } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import {
  useAppGovernanceQuery,
  useSubmitGovernanceAppealMutation,
  useWithdrawGovernanceAppealMutation
} from "@/lib/platform-governance-hooks";
import type { GovernanceRestrictions, GovernanceState } from "@/lib/api/platform-governance";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";

/**
 * 应用被平台治理时的横幅。
 *
 * 为什么必须有这个东西：治理生效后，应用管理员看到的是一连串 403 ——
 * 登录不了、发不出信、改配置报错，却不知道发生了什么。这块横幅是他
 * 在控制台里唯一能读到「被怎么了、为什么、到什么时候、怎么申诉」的地方。
 *
 * 正常状态下整个组件不渲染，不占版面。
 */

const STATE_LABEL: Record<GovernanceState, string> = {
  active: "正常",
  restricted: "部分受限",
  frozen: "冻结",
  suspended: "停运",
  banned: "封禁",
  archived: "归档"
};

const CAPABILITY_LABEL: { field: keyof GovernanceRestrictions; label: string }[] = [
  { field: "blockLogin", label: "登录" },
  { field: "blockRegister", label: "注册" },
  { field: "blockApi", label: "业务接口" },
  { field: "blockPayment", label: "支付下单" },
  { field: "blockStorage", label: "文件上传" },
  { field: "blockNotification", label: "对外通知" },
  { field: "blockAdminWrite", label: "管理端写操作" }
];

function formatDateTime(value?: string | null) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(date);
}

export function AppGovernanceNotice({ appKey }: { appKey?: string | null }) {
  const detailQuery = useAppGovernanceQuery(appKey ?? null);
  const submitMutation = useSubmitGovernanceAppealMutation();
  const withdrawMutation = useWithdrawGovernanceAppealMutation();
  const [appealOpen, setAppealOpen] = useState(false);
  const [content, setContent] = useState("");

  const detail = detailQuery.data;
  if (!appKey || !detail || detail.governance.state === "active") {
    return null;
  }

  const { governance, pendingAppeal } = detail;
  const blocked = CAPABILITY_LABEL.filter((item) => governance.restrictions[item.field]).map(
    (item) => item.label
  );

  const submit = async () => {
    try {
      await submitMutation.mutateAsync({ appKey, content });
      toast.success("申诉已提交，等待平台审核");
      setContent("");
      setAppealOpen(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "提交申诉失败");
    }
  };

  const withdraw = async () => {
    if (!pendingAppeal) return;
    try {
      await withdrawMutation.mutateAsync({ appKey, appealId: pendingAppeal.id });
      toast.success("申诉已撤回");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "撤回申诉失败");
    }
  };

  return (
    <div className="space-y-3 rounded-lg border border-destructive/40 bg-destructive/5 p-4">
      <div className="flex items-start gap-3">
        <ShieldAlert className="mt-0.5 size-5 shrink-0 text-destructive" />
        <div className="min-w-0 flex-1 space-y-1">
          <div className="text-sm font-medium text-foreground">
            该应用已被平台{STATE_LABEL[governance.state] ?? governance.state}
            {governance.endAt ? ` · 预计 ${formatDateTime(governance.endAt)} 恢复` : " · 需人工解除"}
          </div>
          {governance.reason ? (
            <p className="text-sm text-muted-foreground">{governance.reason}</p>
          ) : null}
          {blocked.length > 0 ? (
            <p className="text-xs text-muted-foreground">当前停用：{blocked.join(" · ")}</p>
          ) : null}
        </div>
      </div>

      {pendingAppeal ? (
        <div className="rounded-md bg-background/60 p-3 text-xs text-muted-foreground">
          已于 {formatDateTime(pendingAppeal.createdAt)} 提交申诉，等待平台处理。
          <Button
            size="sm"
            variant="ghost"
            className="ml-2 h-6 px-2 text-xs"
            onClick={withdraw}
            disabled={withdrawMutation.isPending}
          >
            撤回
          </Button>
        </div>
      ) : appealOpen ? (
        <div className="space-y-2">
          <Textarea
            rows={3}
            value={content}
            onChange={(event) => setContent(event.target.value)}
            placeholder="说明情况与已采取的整改措施（不少于 10 个字）"
          />
          <div className="flex justify-end gap-2">
            <Button size="sm" variant="ghost" onClick={() => setAppealOpen(false)}>
              取消
            </Button>
            <Button size="sm" onClick={submit} disabled={submitMutation.isPending}>
              {submitMutation.isPending ? "提交中…" : "提交申诉"}
            </Button>
          </div>
        </div>
      ) : (
        <Button size="sm" variant="outline" onClick={() => setAppealOpen(true)}>
          提交申诉
        </Button>
      )}
    </div>
  );
}
