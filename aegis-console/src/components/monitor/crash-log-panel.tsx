"use client";

import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Download, Eye, FileText, Loader2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useAuthStore } from "@/lib/auth-store";
import { getCrashLogs, getCrashLogContent, deleteCrashLog, type CrashLogFile } from "@/lib/api/system";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { SurfaceCard } from "@/components/ui/surface-card";
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
  const operator = useAuthStore((s) => s.operator);
  const queryClient = useQueryClient();
  const [selectedFile, setSelectedFile] = useState<string | null>(null);
  const [content, setContent] = useState<string>("");
  const [loadingContent, setLoadingContent] = useState(false);

  const filesQuery = useQuery({
    queryKey: ["admin", "crashlogs"],
    queryFn: () => getCrashLogs(token!),
    enabled: Boolean(token && operator?.isSuperAdmin),
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
      setContent("Failed to load crash log content.");
    } finally {
      setLoadingContent(false);
    }
  }

  if (!operator?.isSuperAdmin) {
    return <EmptyState title="No access" description="Only super admins can view crash logs." />;
  }

  if (filesQuery.isLoading) {
    return <LoadingState title="Loading crash logs" />;
  }

  const files: CrashLogFile[] = filesQuery.data ?? [];
  const records = selectedFile ? parseCrashRecords(content) : [];

  return (
    <>
      <SurfaceCard>
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <AlertTriangle className="size-5 text-destructive" />
            <h2 className="text-lg font-semibold">Crash Logs</h2>
            <span className="text-sm text-muted-foreground">
              ({files.length} file{files.length !== 1 ? "s" : ""})
            </span>
          </div>

          {files.length === 0 ? (
            <div className="rounded-xl border border-dashed px-6 py-10 text-center text-sm text-muted-foreground">
              No crash logs found. System is healthy.
            </div>
          ) : (
            <div className="space-y-2">
              {files.map((f) => (
                <div
                  key={f.name}
                  className="flex items-center justify-between rounded-xl border px-4 py-3 text-sm"
                >
                  <div className="flex items-center gap-3">
                    <FileText className="size-4 text-muted-foreground" />
                    <span className="font-mono">{f.name}</span>
                    <span className="text-muted-foreground">{formatBytes(f.size)}</span>
                    <span className="text-muted-foreground">
                      {new Date(f.modTime).toLocaleString()}
                    </span>
                  </div>
                  <div className="flex items-center gap-1">
                    <Button variant="ghost" size="sm" className="h-7 gap-1" onClick={() => handleView(f.name)}>
                      <Eye className="size-3.5" />
                      View
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 gap-1 text-destructive hover:text-destructive"
                      disabled={deleteMutation.isPending}
                      onClick={() => deleteMutation.mutate(f.name)}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </SurfaceCard>

      <Dialog open={!!selectedFile} onOpenChange={(open) => { if (!open) setSelectedFile(null); }}>
        <DialogContent className="max-w-4xl max-h-[80vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle className="font-mono text-sm">{selectedFile}</DialogTitle>
            <DialogDescription>
              {records.length} crash record{records.length !== 1 ? "s" : ""}
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
                      {new Date(rec.timestamp).toLocaleString()}
                    </span>
                    {rec.recovered ? (
                      <span className="text-xs text-emerald-600 dark:text-emerald-400">recovered</span>
                    ) : (
                      <span className="text-xs text-destructive">process exited</span>
                    )}
                    <span className="text-xs text-muted-foreground">
                      pid={rec.pid} goroutine={rec.goroutine} uptime={rec.uptime}
                    </span>
                  </div>
                  <div className="rounded-lg bg-destructive/5 px-3 py-2 font-mono text-destructive text-xs break-all">
                    {rec.error}
                  </div>
                  <details className="group">
                    <summary className="cursor-pointer text-xs text-muted-foreground hover:text-foreground">
                      Stack trace
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
              {content || "Empty file"}
            </pre>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
