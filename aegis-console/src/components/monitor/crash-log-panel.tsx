"use client";

import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Eye, FileText, Loader2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useAuthStore } from "@/lib/auth-store";
import { usePermissionChecker } from "@/lib/permissions";
import { formatDateTime } from "@/lib/datetime";
import { getCrashLogs, getCrashLogContent, deleteCrashLog, type CrashLogFile } from "@/lib/api/system";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}

interface CrashRecord {
  timestamp: string;
  level: string;
  component: string;
  error: string;
  goroutine: number;
  stack_trace: string;
  recovered: boolean;
  pid: number;
  uptime: string;
}

function parseCrashRecords(text: string): CrashRecord[] {
  return text
    .split("\n")
    .filter((line) => line.trim().startsWith("{"))
    .map((line) => {
      try {
        return JSON.parse(line) as CrashRecord;
      } catch {
        return null;
      }
    })
    .filter(Boolean) as CrashRecord[];
}

export function CrashLogPanel() {
  const token = useAuthStore((s) => s.accessToken);
  const can = usePermissionChecker();
  const canRead = can("system:crashlog:read");
  const canDelete = can("system:crashlog:delete");
  const queryClient = useQueryClient();
  const [selectedFile, setSelectedFile] = useState<string | null>(null);
  const [content, setContent] = useState<string>("");
  const [loadingContent, setLoadingContent] = useState(false);

  const filesQuery = useQuery({
    queryKey: ["admin", "crashlogs"],
    queryFn: () => getCrashLogs(token!),
    enabled: Boolean(token && canRead),
    refetchInterval: 30_000,
  });

  const deleteMutation = useMutation({
    mutationFn: (filename: string) => deleteCrashLog(token!, filename),
    onSuccess: () => {
      toast.success("崩溃日志已删除");
      queryClient.invalidateQueries({ queryKey: ["admin", "crashlogs"] });
      if (selectedFile) setSelectedFile(null);
    },
    onError: () => toast.error("删除失败"),
  });

  async function handleView(filename: string) {
    setLoadingContent(true);
    setSelectedFile(filename);
    try {
      const text = await getCrashLogContent(token!, filename);
      setContent(text);
    } catch {
      setContent("崩溃日志读取失败");
    } finally {
      setLoadingContent(false);
    }
  }

  if (!canRead) {
    return <EmptyState title="缺少「服务端崩溃日志：查看」权限" />;
  }

  if (filesQuery.isLoading) {
    return <LoadingState title="加载崩溃日志" />;
  }

  const files: CrashLogFile[] = filesQuery.data ?? [];
  const records = selectedFile ? parseCrashRecords(content) : [];

  return (
    <>
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <AlertTriangle className="size-4 text-muted-foreground" />
          <h3 className="text-sm font-semibold">崩溃日志</h3>
          <span className="text-xs text-muted-foreground">{files.length} 个文件</span>
        </div>

        {files.length === 0 ? (
          <div className="rounded-lg border border-dashed px-6 py-10 text-center text-xs text-muted-foreground">
            暂无崩溃日志
          </div>
        ) : (
          <ul className="divide-y rounded-lg border">
            {files.map((f) => (
              <li key={f.name} className="flex items-center gap-3 px-4 py-2.5 text-xs">
                <FileText className="size-4 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate font-mono">{f.name}</span>
                <span className="hidden shrink-0 text-muted-foreground sm:inline">{formatBytes(f.size)}</span>
                <span className="shrink-0 font-data text-muted-foreground tabular-nums">{formatDateTime(f.modTime)}</span>
                <span className="flex shrink-0 items-center gap-1">
                  <Button variant="ghost" size="xs" onClick={() => handleView(f.name)}>
                    <Eye />
                    查看
                  </Button>
                  {canDelete ? (
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label="删除"
                      className="text-destructive hover:text-destructive"
                      disabled={deleteMutation.isPending}
                      onClick={() => deleteMutation.mutate(f.name)}
                    >
                      <Trash2 />
                    </Button>
                  ) : null}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <Dialog open={!!selectedFile} onOpenChange={(open) => { if (!open) setSelectedFile(null); }}>
        <DialogContent className="max-w-4xl max-h-[80vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle className="font-mono text-sm">{selectedFile}</DialogTitle>
            <DialogDescription>
              共 {records.length} 条崩溃记录
            </DialogDescription>
          </DialogHeader>

          {loadingContent ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="size-6 animate-spin text-muted-foreground" />
            </div>
          ) : records.length > 0 ? (
            <div className="flex-1 overflow-y-auto space-y-3 pr-2">
              {records.map((rec, i) => (
                <div key={i} className="rounded-xl border p-4 space-y-2 text-sm">
                  <div className="flex items-center gap-3 flex-wrap">
                    <span
                      className={`rounded px-1.5 py-0.5 text-xs font-semibold ${
                        rec.level === "FATAL"
                          ? "bg-destructive/15 text-destructive"
                          : "bg-amber-500/15 text-amber-600 dark:text-amber-400"
                      }`}
                    >
                      {rec.level}
                    </span>
                    <span className="font-mono text-muted-foreground">{rec.component}</span>
                    <span className="text-muted-foreground">
                      {formatDateTime(rec.timestamp)}
                    </span>
                    {rec.recovered ? (
                      <span className="text-xs text-emerald-600 dark:text-emerald-400">已恢复</span>
                    ) : (
                      <span className="text-xs text-destructive">进程已退出</span>
                    )}
                    <span className="text-xs text-muted-foreground">
                      进程 {rec.pid}，协程 {rec.goroutine}，已运行 {rec.uptime}
                    </span>
                  </div>
                  <div className="rounded-lg bg-destructive/5 px-3 py-2 font-mono text-destructive text-xs break-all">
                    {rec.error}
                  </div>
                  <details className="group">
                    <summary className="cursor-pointer text-xs text-muted-foreground hover:text-foreground">
                      调用栈
                    </summary>
                    <pre className="mt-2 max-h-60 overflow-auto rounded-lg bg-muted/50 p-3 text-[11px] leading-relaxed font-mono whitespace-pre-wrap break-all">
                      {rec.stack_trace}
                    </pre>
                  </details>
                </div>
              ))}
            </div>
          ) : (
            <pre className="flex-1 overflow-auto rounded-lg bg-muted/50 p-4 text-xs font-mono whitespace-pre-wrap">
              {content || "文件为空"}
            </pre>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
