"use client";

import { useState } from "react";
import { LayoutGrid, Loader2, MonitorSmartphone, Plus, Save, ShieldCheck, X } from "lucide-react";
import { toast } from "sonner";
import {
  FieldGroup,
  NumberField,
  SectionCard,
  StatusDot,
  SwitchRow
} from "@/components/apps/app-config-primitives";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import type { AdPolicy } from "@/lib/api/ad-policy";
import { useAdPolicyQuery, useSaveAdPolicyMutation } from "@/lib/ad-policy-hooks";

/** 与后端 adpolicy 的 toolIDPattern 一致：小写字母、数字与 _ . : -，最长 64 位。 */
const TOOL_ID_PATTERN = /^[a-z0-9][a-z0-9_.:-]{0,63}$/;
const MAX_BASIC_TOOLS = 200;

/** 数字项按字符串存，理由同激励广告配置：存 number 的话输入框清不空。 */
type Draft = {
  scope: string;
  enabled: boolean;
  consentVersion: string;
  policyUrl: string;
  vipExempt: boolean;
  basicTools: string[];
  splashEnabled: boolean;
  splashPlacementId: string;
  splashMinIntervalSeconds: string;
  splashDailyLimit: string;
};

function seedDraft(scope: string, policy?: AdPolicy): Draft {
  return {
    scope,
    enabled: policy?.enabled ?? false,
    consentVersion: String(policy?.consentVersion ?? 1),
    policyUrl: policy?.policyUrl ?? "",
    vipExempt: policy?.vipExempt ?? true,
    basicTools: policy?.basicTools ?? [],
    splashEnabled: policy?.splash.enabled ?? true,
    splashPlacementId: policy?.splash.placementId ?? "",
    splashMinIntervalSeconds: String(policy?.splash.minIntervalSeconds ?? 0),
    splashDailyLimit: String(policy?.splash.dailyLimit ?? 0)
  };
}

/** 一次粘贴多个标识：逗号、顿号、空白与换行都算分隔。 */
function splitToolIds(raw: string) {
  return raw
    .split(/[\s,，、;；]+/)
    .map((item) => item.trim().toLowerCase())
    .filter(Boolean);
}

export function AdPolicyConfigPanel({ appKey }: { appKey: string }) {
  const policyQuery = useAdPolicyQuery(appKey);
  const saveMutation = useSaveAdPolicyMutation(appKey);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [toolInput, setToolInput] = useState("");
  const [confirmBump, setConfirmBump] = useState(false);

  const policy = policyQuery.data;
  const current = draft?.scope === appKey ? draft : seedDraft(appKey, policy);
  const patch = (changes: Partial<Draft>) => setDraft({ ...current, ...changes, scope: appKey });
  const savedVersion = policy?.consentVersion ?? 1;

  const addTools = () => {
    const ids = splitToolIds(toolInput);
    if (ids.length === 0) return;
    const invalid = ids.find((id) => !TOOL_ID_PATTERN.test(id));
    if (invalid) {
      toast.error(`功能标识「${invalid}」无效`);
      return;
    }
    const next = [...current.basicTools];
    ids.forEach((id) => {
      if (!next.includes(id)) next.push(id);
    });
    if (next.length > MAX_BASIC_TOOLS) {
      toast.error(`基础服务最多 ${MAX_BASIC_TOOLS} 项`);
      return;
    }
    patch({ basicTools: next });
    setToolInput("");
  };

  const save = async () => {
    try {
      await saveMutation.mutateAsync({
        enabled: current.enabled,
        consentVersion: Number(current.consentVersion) || 1,
        policyUrl: current.policyUrl.trim(),
        vipExempt: current.vipExempt,
        basicTools: current.basicTools,
        splash: {
          enabled: current.splashEnabled,
          placementId: current.splashPlacementId.trim(),
          minIntervalSeconds: Number(current.splashMinIntervalSeconds) || 0,
          dailyLimit: Number(current.splashDailyLimit) || 0
        }
      });
      toast.success("广告服务策略已保存");
      setDraft(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  const requestSave = () => {
    const version = Number(current.consentVersion) || 1;
    if (version < savedVersion) {
      toast.error(`条款版本不能低于 ${savedVersion}`);
      return;
    }
    // 调高版本会让所有人回到「需要重新选择」，这一步值得停下来确认一次
    if (policy?.configured && version > savedVersion) {
      setConfirmBump(true);
      return;
    }
    void save();
  };

  if (policyQuery.isLoading) {
    return (
      <div className="space-y-4">
        {[0, 1, 2].map((index) => (
          <Skeleton key={index} className="h-40 w-full rounded-2xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <SectionCard
        icon={<ShieldCheck className="size-4" />}
        title="同意要求"
        aside={
          policy?.configured ? (
            <StatusDot active={Boolean(policy.enabled)} labelActive="已启用" labelInactive="未启用" />
          ) : (
            <span className="text-xs text-muted-foreground">未配置，客户端使用内置策略</span>
          )
        }
      >
        <div className="space-y-5">
          <div className="grid gap-3 sm:grid-cols-2">
            <SwitchRow
              label="拒绝时仅提供基础服务"
              checked={current.enabled}
              onChange={(value) => patch({ enabled: value })}
            />
            <SwitchRow label="会员免除" checked={current.vipExempt} onChange={(value) => patch({ vipExempt: value })} />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <NumberField
              label="条款版本"
              min={savedVersion}
              max={1000000}
              hint={`当前 ${savedVersion}`}
              value={current.consentVersion}
              onChange={(value) => patch({ consentVersion: value })}
            />
            <FieldGroup label="条款链接">
              <Input
                className="h-8 text-xs"
                value={current.policyUrl}
                placeholder="https://"
                onChange={(event) => patch({ policyUrl: event.target.value })}
              />
            </FieldGroup>
          </div>
        </div>
      </SectionCard>

      <SectionCard
        icon={<LayoutGrid className="size-4" />}
        title="基础服务"
        aside={<span className="text-xs tabular-nums text-muted-foreground">{current.basicTools.length} 项</span>}
      >
        <div className="space-y-3">
          <div className="flex gap-2">
            <Input
              className="h-8 font-mono text-xs"
              value={toolInput}
              placeholder="功能标识，如 todo、stopwatch"
              onChange={(event) => setToolInput(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  addTools();
                }
              }}
            />
            <Button size="sm" variant="outline" onClick={addTools} disabled={!toolInput.trim()}>
              <Plus className="size-3.5" />
              添加
            </Button>
          </div>
          {current.basicTools.length === 0 ? (
            <p className="rounded-xl border border-dashed border-border px-4 py-6 text-center text-xs text-muted-foreground">
              暂无基础服务
            </p>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {current.basicTools.map((tool) => (
                <Badge key={tool} variant="outline" className="gap-1 pr-1 font-mono font-normal">
                  {tool}
                  <button
                    type="button"
                    className="rounded-sm text-muted-foreground hover:text-foreground"
                    onClick={() => patch({ basicTools: current.basicTools.filter((item) => item !== tool) })}
                    aria-label={`移除 ${tool}`}
                  >
                    <X className="size-3" />
                  </button>
                </Badge>
              ))}
            </div>
          )}
        </div>
      </SectionCard>

      <SectionCard
        icon={<MonitorSmartphone className="size-4" />}
        title="开屏广告"
        footer={
          <div className="flex items-center justify-between gap-3">
            <span className="text-[11px] text-muted-foreground">
              {policy?.configured && policy.updatedAt
                ? `上次保存 ${new Date(policy.updatedAt).toLocaleString("zh-CN")}`
                : "尚未保存"}
              {policy?.updatedBy ? ` · ${policy.updatedBy}` : ""}
            </span>
            <Button size="sm" onClick={requestSave} disabled={saveMutation.isPending}>
              {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
              保存
            </Button>
          </div>
        }
      >
        <div className="space-y-5">
          <SwitchRow
            label="启用开屏广告"
            checked={current.splashEnabled}
            onChange={(value) => patch({ splashEnabled: value })}
          />
          <div className="grid gap-4 sm:grid-cols-3">
            <FieldGroup label="广告位 ID">
              <Input
                className="h-8 font-mono text-xs"
                value={current.splashPlacementId}
                placeholder="留空使用客户端内置"
                onChange={(event) => patch({ splashPlacementId: event.target.value })}
              />
            </FieldGroup>
            <NumberField
              label="最小间隔"
              unit="秒"
              min={0}
              max={86400}
              hint="0 表示不限"
              value={current.splashMinIntervalSeconds}
              onChange={(value) => patch({ splashMinIntervalSeconds: value })}
            />
            <NumberField
              label="每人每日次数"
              unit="次"
              min={0}
              max={100}
              hint="0 表示不限"
              value={current.splashDailyLimit}
              onChange={(value) => patch({ splashDailyLimit: value })}
            />
          </div>
        </div>
      </SectionCard>

      <AlertDialog open={confirmBump} onOpenChange={setConfirmBump}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>调高条款版本</AlertDialogTitle>
            <AlertDialogDescription>所有用户需按新版本重新选择，此前同意的用户在重新选择前仅有基础服务。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmBump(false);
                void save();
              }}
            >
              保存
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
