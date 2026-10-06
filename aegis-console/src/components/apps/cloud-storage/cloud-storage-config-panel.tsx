"use client";

import { useState } from "react";
import { Cloud, Eraser, FolderTree, Gauge, Loader2, Plus, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, NumberField, SectionCard, StatusDot, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  bytesToMBInput,
  describeTarget,
  formatBytes,
  mbInputToBytes,
  providerLabel
} from "@/components/apps/cloud-storage/cloud-storage-shared";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import type { CloudStorageConfig } from "@/lib/api/cloud-storage";
import {
  useCloudStorageConfigQuery,
  usePurgeExpiredCloudTrashMutation,
  useSaveCloudStorageConfigMutation
} from "@/lib/cloud-storage-hooks";

/** 与后端 cloudstorage.ValidNamespace 同一条规则。 */
const NAMESPACE_RE = /^[a-z0-9][a-z0-9._-]{0,63}$/;
/** Select 不接受空串作值，「应用默认」用一个占位值表示。 */
const DEFAULT_TARGET = "__default__";

type NamespaceDraft = { uid: string; key: string; name: string; description: string };

/** 数字项按字符串存：存 number 的话输入框清不空（理由同激励广告配置）。 */
type Draft = {
  scope: string;
  enabled: boolean;
  storageConfigName: string;
  quotaMB: string;
  maxItemMB: string;
  maxItems: string;
  maxRevisions: string;
  trashRetentionDays: string;
  restrictNamespaces: boolean;
  namespaces: NamespaceDraft[];
};

let uidSeed = 0;
const nextUid = () => `ns-${++uidSeed}`;

function seedDraft(scope: string, config?: CloudStorageConfig): Draft {
  return {
    scope,
    enabled: config?.enabled ?? false,
    storageConfigName: config?.storageConfigName ?? "",
    quotaMB: bytesToMBInput(config?.quotaBytes ?? 20 * 1024 * 1024),
    maxItemMB: bytesToMBInput(config?.maxItemBytes ?? 1024 * 1024),
    maxItems: String(config?.maxItems ?? 500),
    maxRevisions: String(config?.maxRevisions ?? 10),
    trashRetentionDays: String(config?.trashRetentionDays ?? 30),
    restrictNamespaces: config?.restrictNamespaces ?? false,
    // 从服务端数据播种的行用下标作 uid：随机 uid 会让输入框在每次重渲染时被卸载重建。
    namespaces: (config?.namespaces ?? []).map((item, index) => ({
      uid: `seed-${index}`,
      key: item.key,
      name: item.name,
      description: item.description ?? ""
    }))
  };
}

export function CloudStorageConfigPanel({ appKey }: { appKey: string }) {
  const configQuery = useCloudStorageConfigQuery(appKey);
  const saveMutation = useSaveCloudStorageConfigMutation(appKey);
  const purgeMutation = usePurgeExpiredCloudTrashMutation(appKey);
  const [draft, setDraft] = useState<Draft | null>(null);

  const config = configQuery.data;
  const current = draft?.scope === appKey ? draft : seedDraft(appKey, config);
  const patch = (changes: Partial<Draft>) => setDraft({ ...current, ...changes, scope: appKey });
  const patchNamespace = (uid: string, changes: Partial<NamespaceDraft>) =>
    patch({ namespaces: current.namespaces.map((item) => (item.uid === uid ? { ...item, ...changes } : item)) });

  // 后端按名称解析（应用级优先于平台级），同名的两项实际是同一个选择，只保留先出现的那一项。
  const options = (config?.storageOptions ?? []).filter(
    (option, index, all) => all.findIndex((entry) => entry.configName === option.configName) === index
  );
  const selected = options.find((option) => option.configName === current.storageConfigName);
  const caps = config?.caps;

  const save = async () => {
    const quotaBytes = mbInputToBytes(current.quotaMB);
    const maxItemBytes = mbInputToBytes(current.maxItemMB);
    if (Number.isNaN(quotaBytes) || Number.isNaN(maxItemBytes)) {
      toast.error("配额与单条目上限需为正数");
      return;
    }
    if (maxItemBytes > quotaBytes) {
      toast.error("单条目上限不能超过单用户配额");
      return;
    }
    const seen = new Set<string>();
    for (const item of current.namespaces) {
      const key = item.key.trim();
      if (!NAMESPACE_RE.test(key)) {
        toast.error(`命名空间「${key || "未填写"}」格式无效`);
        return;
      }
      if (seen.has(key)) {
        toast.error(`命名空间「${key}」重复`);
        return;
      }
      seen.add(key);
    }
    if (current.restrictNamespaces && current.namespaces.length === 0) {
      toast.error("限定命名空间时目录不能为空");
      return;
    }
    try {
      await saveMutation.mutateAsync({
        enabled: current.enabled,
        storageConfigName: current.storageConfigName,
        quotaBytes,
        maxItemBytes,
        maxItems: Number(current.maxItems) || 0,
        maxRevisions: Number(current.maxRevisions) || 0,
        trashRetentionDays: Number(current.trashRetentionDays) || 0,
        restrictNamespaces: current.restrictNamespaces,
        namespaces: current.namespaces.map((item) => ({
          key: item.key.trim(),
          name: item.name.trim() || item.key.trim(),
          description: item.description.trim() || undefined
        }))
      });
      toast.success("云存储配置已保存");
      setDraft(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  const purge = async () => {
    try {
      const result = await purgeMutation.mutateAsync();
      toast.success(result.purged > 0 ? `已清理 ${result.purged} 个过期条目` : "没有过期条目");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "清理失败");
    }
  };

  if (configQuery.isLoading) {
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
        icon={<Cloud className="size-4" />}
        title="开关与存储"
        aside={<StatusDot active={Boolean(config?.enabled)} labelActive="已启用" labelInactive="未启用" />}
      >
        <div className="space-y-5">
          <SwitchRow
            label="启用云存储"
            hint="关闭后客户端入口隐藏，已有数据保留"
            checked={current.enabled}
            onChange={(value) => patch({ enabled: value })}
          />

          <FieldGroup label="存储配置" hint="内容写入的位置">
            <Select
              value={current.storageConfigName || DEFAULT_TARGET}
              onValueChange={(value) => patch({ storageConfigName: value === DEFAULT_TARGET ? "" : value })}
            >
              <SelectTrigger className="h-8 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={DEFAULT_TARGET}>应用默认存储</SelectItem>
                {options.map((option) => (
                  <SelectItem key={option.configId} value={option.configName} disabled={!option.enabled}>
                    {option.configName} · {providerLabel(option.provider)} ·{" "}
                    {option.scope === "global" ? "平台级" : "应用级"}
                    {option.accessMode === "public" ? " · 公开" : ""}
                    {!option.enabled ? " · 未启用" : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-[11px] text-muted-foreground">
              当前解析：{describeTarget(config?.target)}
              {config?.target?.error ? <span className="text-destructive"> · {config.target.error}</span> : null}
            </p>
            {selected?.accessMode === "public" ||
            (!current.storageConfigName &&
              options.find((option) => option.configId === config?.target?.configId)?.accessMode === "public") ? (
              <p className="text-[11px] text-amber-600 dark:text-amber-400">
                公开访问的存储配置可被直接访问，建议为云存储选择私有配置
              </p>
            ) : null}
          </FieldGroup>
        </div>
      </SectionCard>

      <SectionCard icon={<Gauge className="size-4" />} title="配额与限制">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <NumberField
            label="每人配额"
            unit="MB"
            min={0}
            max={caps ? caps.maxQuotaBytes / 1024 / 1024 : undefined}
            hint="含历史修订与回收站"
            value={current.quotaMB}
            onChange={(value) => patch({ quotaMB: value })}
          />
          <NumberField
            label="单条目上限"
            unit="MB"
            min={0}
            max={caps ? caps.maxItemBytes / 1024 / 1024 : undefined}
            hint={caps ? `最大 ${formatBytes(caps.maxItemBytes)}` : undefined}
            value={current.maxItemMB}
            onChange={(value) => patch({ maxItemMB: value })}
          />
          <NumberField
            label="条目数上限"
            unit="个"
            min={1}
            max={caps?.maxItems}
            hint="每人，不含回收站"
            value={current.maxItems}
            onChange={(value) => patch({ maxItems: value })}
          />
          <NumberField
            label="保留修订"
            unit="个"
            min={1}
            max={caps?.maxRevisions}
            hint="每个条目，含当前版本"
            value={current.maxRevisions}
            onChange={(value) => patch({ maxRevisions: value })}
          />
          <NumberField
            label="回收站保留"
            unit="天"
            min={0}
            max={caps?.maxTrashRetentionDays}
            hint="0 表示删除即清除"
            value={current.trashRetentionDays}
            onChange={(value) => patch({ trashRetentionDays: value })}
          />
        </div>
      </SectionCard>

      <SectionCard
        icon={<FolderTree className="size-4" />}
        title="命名空间"
        aside={
          <Button
            size="sm"
            variant="outline"
            disabled={caps ? current.namespaces.length >= caps.maxNamespaces : false}
            onClick={() =>
              patch({ namespaces: [...current.namespaces, { uid: nextUid(), key: "", name: "", description: "" }] })
            }
          >
            <Plus className="size-3.5" />
            添加
          </Button>
        }
        footer={
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span className="text-[11px] text-muted-foreground">
              {config?.configured && config.updatedAt
                ? `上次保存 ${new Date(config.updatedAt).toLocaleString("zh-CN")}`
                : "尚未保存"}
              {config?.updatedBy ? ` · ${config.updatedBy}` : ""}
            </span>
            <div className="flex items-center gap-2">
              <Button size="sm" variant="outline" onClick={() => void purge()} disabled={purgeMutation.isPending}>
                {purgeMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Eraser className="size-3.5" />}
                清理过期回收站
              </Button>
              <Button size="sm" onClick={() => void save()} disabled={saveMutation.isPending}>
                {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
                保存
              </Button>
            </div>
          </div>
        }
      >
        <div className="space-y-4">
          <SwitchRow
            label="仅允许目录内的命名空间"
            hint="关闭时目录只用于显示名称"
            checked={current.restrictNamespaces}
            onChange={(value) => patch({ restrictNamespaces: value })}
          />
          {current.namespaces.length === 0 ? (
            <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
              暂无命名空间
            </p>
          ) : (
            <div className="space-y-2">
              <div className="hidden grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.6fr)_2rem] gap-2 px-1 text-[11px] font-medium text-muted-foreground sm:grid">
                <span>标识</span>
                <span>名称</span>
                <span>说明</span>
                <span />
              </div>
              {current.namespaces.map((item) => {
                const invalid = item.key !== "" && !NAMESPACE_RE.test(item.key);
                return (
                  <div
                    key={item.uid}
                    className="grid gap-2 rounded-xl border border-border p-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.6fr)_2rem] sm:border-0 sm:p-0"
                  >
                    <Input
                      className="h-8 font-mono text-xs"
                      value={item.key}
                      placeholder="favorites"
                      maxLength={64}
                      aria-invalid={invalid}
                      onChange={(event) => patchNamespace(item.uid, { key: event.target.value.toLowerCase().trim() })}
                    />
                    <Input
                      className="h-8 text-xs"
                      value={item.name}
                      placeholder="功能收藏"
                      maxLength={64}
                      onChange={(event) => patchNamespace(item.uid, { name: event.target.value })}
                    />
                    <Input
                      className="h-8 text-xs"
                      value={item.description}
                      placeholder="可选"
                      maxLength={255}
                      onChange={(event) => patchNamespace(item.uid, { description: event.target.value })}
                    />
                    <Button
                      size="icon"
                      variant="ghost"
                      className="size-8 text-muted-foreground hover:text-destructive"
                      aria-label="删除命名空间"
                      onClick={() => patch({ namespaces: current.namespaces.filter((entry) => entry.uid !== item.uid) })}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </SectionCard>
    </div>
  );
}
