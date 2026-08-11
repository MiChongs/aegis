"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  CheckCircle2,
  Code2,
  Loader2,
  Play,
  Plus,
  Trash2,
  UploadCloud
} from "lucide-react";
import { toast } from "sonner";
import { useAdminToken } from "@/lib/admin-hooks";
import { ApiError } from "@/lib/api/client";
import {
  activateAppFunctionVersion,
  createAppFunction,
  createAppFunctionVersion,
  deleteAppFunction,
  invokeAppFunction,
  listAppFunctionInvocations,
  listAppFunctions,
  listAppFunctionVersions,
  updateAppFunction,
  FUNCTION_CAPABILITIES,
  SCRIPT_TEMPLATE,
  type AppFunction,
  type AppFunctionRuntime
} from "@/lib/api/app-functions";
import { ScriptEditor } from "@/components/functions/script-editor";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

function errorMessage(error: unknown) {
  return error instanceof ApiError
    ? error.message
    : error instanceof Error
      ? error.message
      : "操作失败";
}

function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString("zh-CN") : "—";
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 font-mono text-sm">{value}</p>
    </div>
  );
}

function CreateFunctionDialog({
  open,
  onOpenChange,
  appKey,
  onCreated
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string;
  onCreated: () => Promise<unknown>;
}) {
  const token = useAdminToken();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [runtime, setRuntime] = useState<AppFunctionRuntime>("script");
  const [capabilities, setCapabilities] = useState<string[]>(["user.read"]);
  const [busy, setBusy] = useState(false);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>创建远程函数</DialogTitle>
          <DialogDescription>
            函数归属当前应用；创建后需再上传并激活一个版本才能被调用。
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="space-y-2">
            <Label>函数名</Label>
            <Input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="sync-user-profile"
            />
          </div>
          <div className="space-y-2">
            <Label>说明</Label>
            <Input value={description} onChange={(event) => setDescription(event.target.value)} />
          </div>
          <div className="space-y-2">
            <Label>Runtime</Label>
            <Select value={runtime} onValueChange={(value) => setRuntime(value as AppFunctionRuntime)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="script">服务端脚本（JavaScript）</SelectItem>
                <SelectItem value="wasm">WASM 沙箱（纯计算）</SelectItem>
                <SelectItem value="http">HTTP 端点（自建服务）</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {runtime === "script"
                ? "在控制台直接写逻辑，跑在 Aegis 进程内，可读写平台数据。自定义 API 用这个。"
                : runtime === "wasm"
                  ? "纯计算沙箱，拿不到任何平台数据，适合确定性算法。"
                  : "转发到你自建的 HTTPS 端点，需自行实现 Ed25519 双向签名。"}
            </p>
          </div>
          <div className="space-y-2">
            <Label>Capabilities</Label>
            <div className="grid gap-2 sm:grid-cols-2">
              {FUNCTION_CAPABILITIES.map((item) => (
                <label
                  key={item.value}
                  className="flex cursor-pointer items-start gap-2 rounded-lg border p-2.5 text-xs hover:bg-muted/40"
                >
                  <input
                    type="checkbox"
                    className="mt-0.5"
                    checked={capabilities.includes(item.value)}
                    onChange={(event) =>
                      setCapabilities((current) =>
                        event.target.checked
                          ? [...current, item.value]
                          : current.filter((value) => value !== item.value)
                      )
                    }
                  />
                  <span className="min-w-0">
                    <span className="font-medium">{item.label}</span>
                    <code className="ml-1.5 font-mono text-[10px] text-muted-foreground">{item.api}</code>
                    <span className="mt-0.5 block text-[11px] leading-snug text-muted-foreground">
                      {item.hint}
                    </span>
                  </span>
                </label>
              ))}
            </div>
            <p className="text-xs text-muted-foreground">
              声明即授权：没勾选的能力在脚本里根本不存在（<code className="font-mono">aegis.points</code>{" "}
              会是 undefined），编辑器也不会提示它。
            </p>
          </div>
        </div>
        <DialogFooter>
          <Button
            disabled={busy || !name.trim() || !token}
            onClick={async () => {
              if (!token) return;
              setBusy(true);
              try {
                await createAppFunction(token, appKey, {
                  name: name.trim(),
                  description,
                  runtime,
                  capabilities,
                  timeoutMs: 500,
                  maxRequestBytes: 65536,
                  maxResponseBytes: 65536
                });
                await onCreated();
                onOpenChange(false);
                setName("");
                setDescription("");
                setCapabilities([]);
                toast.success("函数已创建");
              } catch (error) {
                toast.error(errorMessage(error));
              } finally {
                setBusy(false);
              }
            }}
          >
            {busy ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
            创建
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CreateVersionDialog({
  open,
  onOpenChange,
  appKey,
  selected,
  onCreated
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string;
  selected: AppFunction;
  onCreated: () => Promise<unknown>;
}) {
  const token = useAdminToken();
  const [version, setVersion] = useState("");
  const [endpointUrl, setEndpointUrl] = useState("");
  const [responsePublicKey, setResponsePublicKey] = useState("");
  const [wasmBase64, setWasmBase64] = useState("");
  const [source, setSource] = useState(SCRIPT_TEMPLATE);
  const [busy, setBusy] = useState(false);

  const isScript = selected.runtime === "script";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={isScript ? "sm:max-w-5xl" : "sm:max-w-2xl"}>
        <DialogHeader>
          <DialogTitle>创建 {selected.name} 版本</DialogTitle>
          <DialogDescription>
            {isScript
              ? "脚本正文只保存在服务端，任何接口都不会下发给接入方。发布前会做语法与入口检查。"
              : selected.runtime === "http"
                ? "HTTP Endpoint 仅允许 HTTPS，禁止重定向，并在连接时重新解析 IP 拒绝内网与元数据地址。"
                : "WASM 最大 2MB，每次调用独立实例，内存上限 16MB，不提供网络与文件系统。"}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <Label>版本</Label>
            <Input
              value={version}
              onChange={(event) => setVersion(event.target.value)}
              placeholder="1.0.0"
              className={isScript ? "max-w-xs" : undefined}
            />
          </div>
          {isScript ? (
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label>脚本</Label>
                <span className="text-xs text-muted-foreground">
                  已声明能力：{selected.capabilities.length ? selected.capabilities.join("、") : "无"}
                </span>
              </div>
              <ScriptEditor
                value={source}
                onChange={setSource}
                capabilities={selected.capabilities}
                height={480}
              />
              <p className="text-xs text-muted-foreground">
                编辑器已载入按当前能力生成的 SDK 类型：输入 <code className="font-mono">aegis.</code>{" "}
                可看到全部可用成员；沙箱里不存在的 DOM/定时器/require 会被标红。
              </p>
            </div>
          ) : selected.runtime === "http" ? (
            <>
              <div className="space-y-2">
                <Label>Endpoint URL</Label>
                <Input
                  value={endpointUrl}
                  onChange={(event) => setEndpointUrl(event.target.value)}
                  placeholder="https://functions.example.com/aegis"
                />
              </div>
              <div className="space-y-2">
                <Label>响应 Ed25519 公钥</Label>
                <Textarea
                  className="min-h-28 font-mono text-xs"
                  value={responsePublicKey}
                  onChange={(event) => setResponsePublicKey(event.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  Worker 必须对响应签名，Aegis 用此公钥验签。
                </p>
              </div>
            </>
          ) : (
            <div className="space-y-2">
              <Label>WASM Base64</Label>
              <Textarea
                className="min-h-64 font-mono text-xs"
                value={wasmBase64}
                onChange={(event) => setWasmBase64(event.target.value)}
              />
            </div>
          )}
        </div>
        <DialogFooter>
          <Button
            disabled={busy || !version.trim() || !token}
            onClick={async () => {
              if (!token) return;
              setBusy(true);
              try {
                await createAppFunctionVersion(token, appKey, selected.name, {
                  version: version.trim(),
                  endpointUrl,
                  responsePublicKey,
                  wasmBase64,
                  source: isScript ? source : undefined
                });
                await onCreated();
                onOpenChange(false);
                setVersion("");
                toast.success("版本已创建");
              } catch (error) {
                toast.error(errorMessage(error));
              } finally {
                setBusy(false);
              }
            }}
          >
            {busy ? <Loader2 className="size-4 animate-spin" /> : <UploadCloud className="size-4" />}
            上传版本
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function FunctionManager({ appKey }: { appKey?: string | null }) {
  const token = useAdminToken();
  const [selectedName, setSelectedName] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [versionOpen, setVersionOpen] = useState(false);
  const [invokeInput, setInvokeInput] = useState('{\n  "action": "ping"\n}');
  const [invokeResult, setInvokeResult] = useState("");
  const [invoking, setInvoking] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<AppFunction | null>(null);

  const enabled = Boolean(token && appKey);

  const functionsQuery = useQuery({
    queryKey: ["app-functions", token, appKey],
    queryFn: () => listAppFunctions(token as string, appKey as string),
    enabled
  });

  const functions = useMemo(() => functionsQuery.data || [], [functionsQuery.data]);

  // selectedName 只记录用户的显式选择；应用切换或函数被删后自动回落到第一个，
  // 因此这里是纯派生，不需要用 effect 去回写 state。
  const selected = functions.find((item) => item.name === selectedName) || functions[0] || null;
  const activeName = selected?.name ?? "";

  const versionsQuery = useQuery({
    queryKey: ["app-function-versions", token, appKey, activeName],
    queryFn: () => listAppFunctionVersions(token as string, appKey as string, activeName),
    enabled: enabled && Boolean(activeName)
  });

  const invocationsQuery = useQuery({
    queryKey: ["app-function-invocations", token, appKey, activeName],
    queryFn: () => listAppFunctionInvocations(token as string, appKey as string, activeName),
    enabled: enabled && Boolean(activeName)
  });

  if (!appKey) {
    return (
      <Card>
        <CardContent className="py-12 text-center text-sm text-muted-foreground">
          请先选择应用。
        </CardContent>
      </Card>
    );
  }

  if (functionsQuery.isLoading) {
    return (
      <div className="flex min-h-48 items-center justify-center">
        <Loader2 className="size-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  const versions = versionsQuery.data || [];
  const invocations = invocationsQuery.data || [];

  async function refreshAll() {
    await Promise.all([
      functionsQuery.refetch(),
      versionsQuery.refetch(),
      invocationsQuery.refetch()
    ]);
  }

  return (
    <div className="grid gap-4 xl:grid-cols-[300px_minmax(0,1fr)]">
      <Card className="h-fit">
        <CardHeader className="flex-row items-center justify-between">
          <CardTitle>函数</CardTitle>
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" />
            创建
          </Button>
        </CardHeader>
        <CardContent className="space-y-2">
          {functions.map((item) => (
            <button
              type="button"
              key={item.id}
              onClick={() => setSelectedName(item.name)}
              className={cn(
                "w-full rounded-lg border p-3 text-left transition-colors",
                activeName === item.name ? "border-primary bg-muted" : "hover:bg-muted/50"
              )}
            >
              <div className="flex items-center justify-between gap-2">
                <span className="min-w-0 truncate font-mono text-sm font-medium">{item.name}</span>
                <Badge variant="outline">{item.runtime}</Badge>
              </div>
              <div className="mt-2 flex items-center justify-between text-xs text-muted-foreground">
                <span>{item.status}</span>
                <span className="truncate">{item.activeVersion || "无活动版本"}</span>
              </div>
            </button>
          ))}
          {!functions.length ? (
            <p className="py-10 text-center text-sm text-muted-foreground">
              暂无函数，点击「创建」开始。
            </p>
          ) : null}
        </CardContent>
      </Card>

      {selected ? (
        <div className="min-w-0 space-y-4">
          <Card>
            <CardHeader className="flex-row items-start justify-between gap-3">
              <div className="min-w-0">
                <CardTitle className="truncate font-mono">{selected.name}</CardTitle>
                <CardDescription>{selected.description || "—"}</CardDescription>
              </div>
              <div className="flex shrink-0 gap-2">
                <Select
                  value={selected.status}
                  onValueChange={async (status) => {
                    if (!token) return;
                    try {
                      await updateAppFunction(token, appKey, selected.name, {
                        status: status as AppFunction["status"]
                      });
                      await functionsQuery.refetch();
                      toast.success("状态已更新");
                    } catch (error) {
                      toast.error(errorMessage(error));
                    }
                  }}
                >
                  <SelectTrigger className="w-32">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="draft">draft</SelectItem>
                    <SelectItem value="active">active</SelectItem>
                    <SelectItem value="disabled">disabled</SelectItem>
                  </SelectContent>
                </Select>
                <Button
                  variant="destructive"
                  size="icon"
                  aria-label={`删除函数 ${selected.name}`}
                  onClick={() => setDeleteTarget(selected)}
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            </CardHeader>
            <CardContent className="grid gap-3 text-sm sm:grid-cols-3">
              <Metric label="超时" value={`${selected.timeoutMs} ms`} />
              <Metric label="请求上限" value={`${selected.maxRequestBytes} bytes`} />
              <Metric label="响应上限" value={`${selected.maxResponseBytes} bytes`} />
              <div className="sm:col-span-3">
                <p className="mb-2 text-xs text-muted-foreground">Capabilities</p>
                <div className="flex flex-wrap gap-1">
                  {selected.capabilities.length ? (
                    selected.capabilities.map((item) => (
                      <Badge key={item} variant="secondary">
                        {item}
                      </Badge>
                    ))
                  ) : (
                    <span className="text-xs text-muted-foreground">未声明</span>
                  )}
                </div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex-row items-center justify-between">
              <div>
                <CardTitle>版本</CardTitle>
                <CardDescription>
                  版本记录不可修改：发布新功能即创建新版本并激活，回滚则重新激活旧版本。
                </CardDescription>
              </div>
              <Button size="sm" onClick={() => setVersionOpen(true)}>
                <UploadCloud className="size-4" />
                新版本
              </Button>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>版本</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>目标</TableHead>
                    <TableHead>SHA-256</TableHead>
                    <TableHead />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {versions.map((version) => (
                    <TableRow key={version.id}>
                      <TableCell className="font-mono">{version.version}</TableCell>
                      <TableCell>
                        <Badge variant={version.status === "active" ? "default" : "outline"}>
                          {version.status}
                        </Badge>
                      </TableCell>
                      <TableCell className="max-w-60 truncate text-xs">
                        {version.endpointUrl || "WASM sandbox"}
                      </TableCell>
                      <TableCell className="max-w-32 truncate font-mono text-xs">
                        {version.artifactSha256}
                      </TableCell>
                      <TableCell className="text-right">
                        {version.status !== "active" ? (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={async () => {
                              if (!token) return;
                              try {
                                await activateAppFunctionVersion(
                                  token,
                                  appKey,
                                  selected.name,
                                  version.version
                                );
                                await refreshAll();
                                toast.success("版本已激活");
                              } catch (error) {
                                toast.error(errorMessage(error));
                              }
                            }}
                          >
                            <CheckCircle2 className="size-4" />
                            激活
                          </Button>
                        ) : null}
                      </TableCell>
                    </TableRow>
                  ))}
                  {!versions.length ? (
                    <TableRow>
                      <TableCell colSpan={5} className="py-10 text-center text-sm text-muted-foreground">
                        暂无版本
                      </TableCell>
                    </TableRow>
                  ) : null}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          <div className="grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>调试调用</CardTitle>
                <CardDescription>以管理员身份走真实执行链，会写入调用审计。</CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                <Textarea
                  className="min-h-44 font-mono text-xs"
                  value={invokeInput}
                  onChange={(event) => setInvokeInput(event.target.value)}
                />
                <Button
                  disabled={invoking}
                  onClick={async () => {
                    if (!token) return;
                    let input: unknown;
                    try {
                      input = JSON.parse(invokeInput);
                    } catch {
                      toast.error("输入不是合法 JSON");
                      return;
                    }
                    setInvoking(true);
                    try {
                      const result = await invokeAppFunction(token, appKey, selected.name, { input });
                      setInvokeResult(JSON.stringify(result, null, 2));
                      await invocationsQuery.refetch();
                      toast.success("调用成功");
                    } catch (error) {
                      setInvokeResult(errorMessage(error));
                      toast.error(errorMessage(error));
                    } finally {
                      setInvoking(false);
                    }
                  }}
                >
                  {invoking ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4" />}
                  执行
                </Button>
                {invokeResult ? (
                  <pre className="max-h-72 overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs">
                    {invokeResult}
                  </pre>
                ) : null}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>最近调用</CardTitle>
                <CardDescription>相同 eventId 重复提交会直接返回既有成功结果。</CardDescription>
              </CardHeader>
              <CardContent className="space-y-2">
                {invocations.slice(0, 10).map((item) => (
                  <div key={item.id} className="rounded-lg border p-3 text-xs">
                    <div className="flex items-center justify-between gap-2">
                      <span className="min-w-0 truncate font-mono">{item.eventId}</span>
                      <Badge variant={item.status === "success" ? "default" : "danger"}>
                        {item.status}
                      </Badge>
                    </div>
                    <div className="mt-2 flex justify-between gap-2 text-muted-foreground">
                      <span>{item.callerType}</span>
                      <span>
                        {item.durationMs.toFixed(2)} ms · {formatTime(item.createdAt)}
                      </span>
                    </div>
                    {item.errorMessage ? (
                      <p className="mt-2 text-destructive">{item.errorMessage}</p>
                    ) : null}
                  </div>
                ))}
                {!invocations.length ? (
                  <p className="py-10 text-center text-sm text-muted-foreground">暂无调用记录</p>
                ) : null}
              </CardContent>
            </Card>
          </div>
        </div>
      ) : (
        <Card>
          <CardContent className="flex min-h-72 flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
            <Code2 className="size-6" />
            选择或创建一个函数。
          </CardContent>
        </Card>
      )}

      <CreateFunctionDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        appKey={appKey}
        onCreated={() => functionsQuery.refetch()}
      />
      {selected ? (
        <CreateVersionDialog
          open={versionOpen}
          onOpenChange={setVersionOpen}
          appKey={appKey}
          selected={selected}
          onCreated={refreshAll}
        />
      ) : null}

      <Dialog open={Boolean(deleteTarget)} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>删除函数</DialogTitle>
            <DialogDescription>
              将删除 <code className="font-mono">{deleteTarget?.name}</code> 及其全部版本，且不可恢复。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)}>
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={async () => {
                if (!token || !deleteTarget) return;
                try {
                  await deleteAppFunction(token, appKey, deleteTarget.name);
                  setDeleteTarget(null);
                  await functionsQuery.refetch();
                  toast.success("函数已删除");
                } catch (error) {
                  toast.error(errorMessage(error));
                }
              }}
            >
              <Trash2 className="size-4" />
              确认删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
