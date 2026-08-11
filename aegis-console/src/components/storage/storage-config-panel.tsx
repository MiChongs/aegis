"use client";

import { useCallback, useState } from "react";
import { Plus, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { StorageConfigForm } from "./storage-config-form";
import { providerOptions } from "./provider-fields";

type StorageConfig = Record<string, unknown>;

type Props = {
  configs: StorageConfig[];
  loading: boolean;
  scope: "global" | "app";
  appId?: number | null;
  onSave: (payload: Record<string, unknown>) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
  onTest: (id: number) => Promise<void>;
  onRefetch: () => void;
  isSaving?: boolean;
  isDeleting?: boolean;
  isTesting?: boolean;
};

function providerLabel(value: string) {
  return providerOptions.find((o) => o.value === value)?.label || value;
}

export function StorageConfigPanel({ configs, loading, scope, appId, onSave, onDelete, onTest, onRefetch, isSaving, isDeleting, isTesting }: Props) {
  const [editConfig, setEditConfig] = useState<StorageConfig | null>(null);
  const [creating, setCreating] = useState(false);

  const handleCreate = useCallback(() => { setEditConfig(null); setCreating(true); }, []);
  const handleEdit = useCallback((cfg: StorageConfig) => { setEditConfig(cfg); setCreating(true); }, []);

  if (loading) {
    return <div className="space-y-3">{Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-10 w-full rounded-lg" />)}</div>;
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <span className="text-xs tabular-nums text-muted-foreground">{configs.length} 个配置</span>
        <div className="flex gap-1.5">
          <Button size="sm" variant="ghost" className="h-7 gap-1 text-xs" onClick={onRefetch}><RefreshCw className="size-3" />刷新</Button>
          <Button size="sm" className="h-7 gap-1 text-xs" onClick={handleCreate}><Plus className="size-3" />新建</Button>
        </div>
      </div>

      {configs.length === 0 ? (
        <div className="py-12 text-center text-sm text-muted-foreground">暂无存储配置</div>
      ) : (
        <div className="overflow-hidden rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>名称</TableHead>
                <TableHead>提供商</TableHead>
                <TableHead>访问</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>默认</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {configs.map((cfg) => (
                <TableRow
                  key={Number(cfg.id)}
                  className="cursor-pointer"
                  onClick={() => handleEdit(cfg)}
                >
                  <TableCell className="text-xs font-medium">{String(cfg.config_name || "—")}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{providerLabel(String(cfg.provider))}</TableCell>
                  <TableCell className="text-xs">{cfg.access_mode === "private" ? "私有" : "公开"}</TableCell>
                  <TableCell>
                    <span className="flex items-center gap-1.5 text-xs">
                      <span className={`inline-block size-1.5 rounded-full ${cfg.enabled !== false ? "bg-emerald-500" : "bg-zinc-300"}`} />
                      {cfg.enabled !== false ? "启用" : "停用"}
                    </span>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">{cfg.is_default ? "是" : "—"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <StorageConfigForm
        open={creating}
        onOpenChange={setCreating}
        editConfig={editConfig}
        scope={scope}
        appId={appId}
        onSave={onSave}
        onDelete={editConfig ? () => onDelete(Number(editConfig.id)) : undefined}
        onTest={editConfig ? () => onTest(Number(editConfig.id)) : undefined}
        isSaving={isSaving}
        isDeleting={isDeleting}
        isTesting={isTesting}
      />
    </div>
  );
}
