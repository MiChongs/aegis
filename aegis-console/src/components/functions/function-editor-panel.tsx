"use client";

import { useMemo, useState } from "react";
import {
  CheckCircle2,
  FlaskConical,
  History,
  Loader2,
  RotateCcw,
  Trash2,
  UploadCloud
} from "lucide-react";
import { toast } from "sonner";
import type {
  AppFunction,
  AppFunctionTestResult,
  FunctionCatalog
} from "@/lib/api/app-functions";
import {
  useActivateVersionMutation,
  useCreateVersionMutation,
  useDeleteVersionMutation,
  useFunctionVersionSourceQuery,
  useFunctionVersionsQuery,
  useTestFunctionMutation
} from "@/lib/function-hooks";
import { getAppFunctionVersion } from "@/lib/api/app-functions";
import { useAdminToken } from "@/lib/admin-hooks";
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
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { EffectList, errorMessage, formatBytes, formatDuration, formatTime } from "./function-shared";

type Draft = { scope: string; source: string };

/**
 * 脚本工作台：写 → 试跑 → 发布 → 回滚，全部在一屏内完成。
 *
 * 重构前这条链路是断的：新建版本的对话框永远从模板开始，
 * 已发布的正文任何接口都取不回来。于是改一行的代价是重写整份脚本，
 * 而验证一行改动的唯一方式是把它激活到线上。
 */
export function FunctionEditorPanel({
  appKey,
  selected,
  catalog,
  initialTemplate
}: {
  appKey: string;
  selected: AppFunction;
  catalog?: FunctionCatalog;
  initialTemplate?: string;
}) {
  const token = useAdminToken();
  const scope = `${appKey}:${selected.name}`;

  const versionsQuery = useFunctionVersionsQuery(appKey, selected.name);
  const versions = useMemo(() => versionsQuery.data ?? [], [versionsQuery.data]);

  // 编辑器的初始正文来自当前激活版本；没有激活版本时用建函数时选的模板。
  const activeSourceQuery = useFunctionVersionSourceQuery(
    appKey,
    selected.name,
    selected.runtime === "script" ? selected.activeVersion || null : null
  );

  const templateSource = useMemo(() => {
    const templates = catalog?.templates ?? [];
    return (
      templates.find((item) => item.key === initialTemplate)?.source ??
      templates.find((item) => item.key === "starter")?.source ??
      ""
    );
  }, [catalog, initialTemplate]);

  // 草稿按 (应用, 函数) 绑定：切换函数时上一个函数的未保存改动不会串过来，
  // 也就不需要一个 effect 去同步（与配置面板同一约束）。
  const [draft, setDraft] = useState<Draft | null>(null);
  const seeded = activeSourceQuery.data?.source ?? templateSource;
  const source = draft?.scope === scope ? draft.source : seeded;
  const dirty = draft?.scope === scope && draft.source !== seeded;

  const [testInput, setTestInput] = useState('{\n  "action": "ping"\n}');
  const [asUserId, setAsUserId] = useState("");
  const [testResult, setTestResult] = useState<AppFunctionTestResult | null>(null);
  const [publishOpen, setPublishOpen] = useState(false);

  const testMutation = useTestFunctionMutation(appKey);
  const activateMutation = useActivateVersionMutation(appKey);
  const deleteVersionMutation = useDeleteVersionMutation(appKey);

  const typeSource = useMemo(
    () =>
      catalog
        ? { baseTypes: catalog.baseTypes, capabilities: catalog.capabilities }
        : undefined,
    [catalog]
  );

  async function loadVersionIntoEditor(version: string) {
    if (!token) return;
    try {
      const detail = await getAppFunctionVersion(token, appKey, selected.name, version);
      setDraft({ scope, source: detail.source });
      toast.success(`已载入版本 ${version} 的正文`);
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  if (selected.runtime !== "script") {
    return (
      <NonScriptVersionPanel
        appKey={appKey}
        selected={selected}
        versions={versions}
        loading={versionsQuery.isLoading}
      />
    );
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader className="flex-row items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2">
              脚本
              {dirty ? (
                <Badge variant="warning" size="sm">
                  未发布的改动
                </Badge>
              ) : null}
            </CardTitle>
            <CardDescription>
              正文只保存在服务端，任何接口都不会下发给接入方。编辑器已按当前能力载入 SDK 类型：
              输入 <code className="font-mono">aegis.</code> 可看到全部可用成员。
            </CardDescription>
          </div>
          <div className="flex shrink-0 gap-2">
            {dirty ? (
              <Button variant="ghost" size="sm" onClick={() => setDraft(null)}>
                <RotateCcw className="size-4" />
                还原
              </Button>
            ) : null}
            <Button size="sm" onClick={() => setPublishOpen(true)} disabled={!source.trim()}>
              <UploadCloud className="size-4" />
              发布版本
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-3">
          <ScriptEditor
            value={source}
            onChange={(next) => setDraft({ scope, source: next })}
            capabilities={selected.capabilities}
            typeSource={typeSource}
            height={460}
          />
          <p className="text-xs text-muted-foreground">
            已声明能力：
            {selected.capabilities.length ? selected.capabilities.join("、") : "无"}
            {catalog ? (
              <>
                {" · "}单次调用额度：SDK {catalog.limits.maxSdkCalls} 次 / 写{" "}
                {catalog.limits.maxSdkMutations} 次 / 出站 {catalog.limits.maxSdkFetches} 次
              </>
            ) : null}
          </p>
        </CardContent>
      </Card>

      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <FlaskConical className="size-4" />
              试跑
            </CardTitle>
            <CardDescription>
              不创建版本、不写调用审计。读的是真实数据，写操作只记录不执行 ——
              因此额度判断、会员判定这些分支都能测到，而不会真的发出去。
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="space-y-2">
              <Label>input</Label>
              <Textarea
                className="min-h-28 font-mono text-xs"
                value={testInput}
                onChange={(event) => setTestInput(event.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label>以哪个用户的身份跑</Label>
              <Input
                value={asUserId}
                onChange={(event) => setAsUserId(event.target.value.replace(/\D/g, ""))}
                placeholder="用户 ID，留空则以当前管理员身份"
                inputMode="numeric"
              />
              <p className="text-[11px] text-muted-foreground">
                多数脚本第一行就是 <code className="font-mono">aegis.user.get()</code>；
                不填的话只能测到那句 fail。
              </p>
            </div>
            <Button
              disabled={testMutation.isPending || !source.trim()}
              onClick={async () => {
                let input: unknown;
                try {
                  input = JSON.parse(testInput);
                } catch {
                  toast.error("input 不是合法 JSON");
                  return;
                }
                try {
                  const result = await testMutation.mutateAsync({
                    name: selected.name,
                    payload: {
                      source,
                      input,
                      asUserId: asUserId ? Number(asUserId) : undefined
                    }
                  });
                  setTestResult(result);
                  // 试跑失败是正常结果，不是接口错误 —— 用 warning 而不是 error，
                  // 免得作者以为是平台出了问题
                  if (result.ok) toast.success(`试跑通过（${formatDuration(result.durationMs)}）`);
                  else toast.warning("试跑未通过，见下方结果");
                } catch (error) {
                  toast.error(errorMessage(error));
                }
              }}
            >
              {testMutation.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <FlaskConical className="size-4" />
              )}
              试跑
            </Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>试跑结果</CardTitle>
            <CardDescription>返回值、日志与本该发生的副作用</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {!testResult ? (
              <p className="py-12 text-center text-sm text-muted-foreground">还没有跑过</p>
            ) : (
              <>
                <div className="flex flex-wrap items-center gap-2 text-xs">
                  <Badge variant={testResult.ok ? "success" : "danger"}>
                    {testResult.ok ? "通过" : testResult.businessCode ? "被脚本拒绝" : "执行失败"}
                  </Badge>
                  <span className="text-muted-foreground">
                    {formatDuration(testResult.durationMs)} · SDK {testResult.sdkCalls} 次 / 写{" "}
                    {testResult.sdkMutations} 次 / 出站 {testResult.sdkFetches} 次
                  </span>
                </div>
                {testResult.error ? (
                  <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-2.5 text-xs">
                    {testResult.businessCode ? (
                      <span className="mr-1.5 font-mono text-[11px] text-muted-foreground">
                        {testResult.businessCode}
                      </span>
                    ) : null}
                    {testResult.error}
                  </div>
                ) : null}
                {testResult.output !== undefined && testResult.output !== null ? (
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground">返回值</p>
                    <pre className="max-h-48 overflow-auto rounded-lg border bg-muted/40 p-2.5 font-mono text-[11px]">
                      {JSON.stringify(testResult.output, null, 2)}
                    </pre>
                  </div>
                ) : null}
                {testResult.logs.length ? (
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground">日志</p>
                    <pre className="max-h-40 overflow-auto rounded-lg border bg-muted/40 p-2.5 font-mono text-[11px]">
                      {testResult.logs.join("\n")}
                    </pre>
                  </div>
                ) : null}
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">副作用</p>
                  <EffectList effects={testResult.effects} />
                </div>
              </>
            )}
          </CardContent>
        </Card>
      </div>

      <VersionTable
        versions={versions}
        loading={versionsQuery.isLoading}
        activeVersion={selected.activeVersion}
        showSource
        onLoadSource={loadVersionIntoEditor}
        onActivate={async (version) => {
          try {
            await activateMutation.mutateAsync({ name: selected.name, version });
            toast.success(`版本 ${version} 已激活`);
          } catch (error) {
            toast.error(errorMessage(error));
          }
        }}
        onDelete={async (version) => {
          try {
            await deleteVersionMutation.mutateAsync({ name: selected.name, version });
            toast.success(`版本 ${version} 已删除`);
          } catch (error) {
            toast.error(errorMessage(error));
          }
        }}
      />

      <PublishDialog
        open={publishOpen}
        onOpenChange={setPublishOpen}
        appKey={appKey}
        selected={selected}
        source={source}
        suggestedVersion={suggestNextVersion(versions.map((item) => item.version))}
        onPublished={() => setDraft(null)}
      />
    </div>
  );
}

/**
 * 版本号建议：把最后一段数字加一。
 *
 * 建议而不是强制 —— 版本号的格式由接入方自己定，平台只负责它唯一且不可变。
 * 但每次都让人手打一遍 `1.0.7` 是没必要的，而手打恰恰会打错。
 */
function suggestNextVersion(existing: string[]) {
  if (!existing.length) return "1.0.0";
  const match = existing[0].match(/^(.*?)(\d+)([^\d]*)$/);
  if (!match) return "";
  return `${match[1]}${Number(match[2]) + 1}${match[3]}`;
}

function PublishDialog({
  open,
  onOpenChange,
  appKey,
  selected,
  source,
  suggestedVersion,
  onPublished
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string;
  selected: AppFunction;
  source: string;
  suggestedVersion: string;
  onPublished: () => void;
}) {
  const [version, setVersion] = useState(suggestedVersion);
  const [notes, setNotes] = useState("");
  // 默认发布即激活：绝大多数情况下发新版本就是为了让它生效，
  // 而「发了却没生效」是最难自己发现的一种状态。
  const [activate, setActivate] = useState(true);
  const createVersion = useCreateVersionMutation(appKey);
  const activateVersion = useActivateVersionMutation(appKey);

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) setVersion(suggestedVersion);
        onOpenChange(next);
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>发布 {selected.name} 的新版本</DialogTitle>
          <DialogDescription>
            版本记录不可修改。发布前会做语法与入口检查，跑不起来的脚本进不了线上。
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <Label>版本号</Label>
            <Input
              value={version}
              onChange={(event) => setVersion(event.target.value)}
              placeholder="1.0.0"
            />
          </div>
          <div className="space-y-2">
            <Label>发版说明</Label>
            <Textarea
              className="min-h-20 text-xs"
              value={notes}
              onChange={(event) => setNotes(event.target.value)}
              placeholder="这一版改了什么 —— 回滚时要靠它决定滚到哪一版"
            />
          </div>
          <label className="flex items-center justify-between rounded-lg border p-2.5 text-sm">
            <span>
              发布后立即激活
              <span className="mt-0.5 block text-[11px] text-muted-foreground">
                关掉的话这一版只是暂存，线上仍跑当前激活的版本
              </span>
            </span>
            <Switch checked={activate} onCheckedChange={setActivate} />
          </label>
          <p className="text-xs text-muted-foreground">
            正文 {formatBytes(new TextEncoder().encode(source).length)}
          </p>
        </div>
        <DialogFooter>
          <Button
            disabled={createVersion.isPending || !version.trim()}
            onClick={async () => {
              try {
                const created = await createVersion.mutateAsync({
                  name: selected.name,
                  payload: { version: version.trim(), source, notes }
                });
                if (activate) {
                  await activateVersion.mutateAsync({
                    name: selected.name,
                    version: created.version
                  });
                }
                onPublished();
                onOpenChange(false);
                setNotes("");
                toast.success(activate ? "版本已发布并激活" : "版本已发布（未激活）");
              } catch (error) {
                toast.error(errorMessage(error));
              }
            }}
          >
            {createVersion.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <UploadCloud className="size-4" />
            )}
            发布
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function VersionTable({
  versions,
  loading,
  activeVersion,
  showSource,
  onLoadSource,
  onActivate,
  onDelete
}: {
  versions: Array<{
    id: number;
    version: string;
    status: string;
    notes: string;
    sourceBytes: number;
    endpointUrl?: string;
    artifactSha256: string;
    createdAt: string;
  }>;
  loading?: boolean;
  activeVersion?: string;
  showSource?: boolean;
  onLoadSource?: (version: string) => void;
  onActivate: (version: string) => void;
  onDelete: (version: string) => void;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>版本</CardTitle>
        <CardDescription>
          版本记录不可修改：发布新功能即创建新版本并激活，回滚则重新激活旧版本。
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>版本</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>说明</TableHead>
              <TableHead>产物</TableHead>
              <TableHead>发布时间</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {versions.map((version) => (
              <TableRow key={version.id}>
                <TableCell className="font-mono">{version.version}</TableCell>
                <TableCell>
                  <Badge variant={version.status === "active" ? "success" : "outline"}>
                    {version.status}
                  </Badge>
                </TableCell>
                <TableCell className="max-w-60 truncate text-xs text-muted-foreground">
                  {version.notes || "—"}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {version.endpointUrl
                    ? version.endpointUrl
                    : version.sourceBytes > 0
                      ? formatBytes(version.sourceBytes)
                      : "WASM"}
                  <span className="ml-1.5 font-mono text-[10px]">
                    {version.artifactSha256.slice(0, 8)}
                  </span>
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {formatTime(version.createdAt)}
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-1">
                    {showSource && onLoadSource ? (
                      <Button
                        size="sm"
                        variant="ghost"
                        title="把这一版的正文载入编辑器"
                        onClick={() => onLoadSource(version.version)}
                      >
                        <History className="size-4" />
                      </Button>
                    ) : null}
                    {version.version !== activeVersion ? (
                      <>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => onActivate(version.version)}
                        >
                          <CheckCircle2 className="size-4" />
                          激活
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          aria-label={`删除版本 ${version.version}`}
                          onClick={() => onDelete(version.version)}
                        >
                          <Trash2 className="size-4 text-destructive" />
                        </Button>
                      </>
                    ) : null}
                  </div>
                </TableCell>
              </TableRow>
            ))}
            {!versions.length ? (
              <TableRow>
                <TableCell colSpan={6} className="py-10 text-center text-sm text-muted-foreground">
                  {loading ? "加载中…" : "暂无版本"}
                </TableCell>
              </TableRow>
            ) : null}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

/** wasm / http 运行时的发布面板：没有脚本编辑器，也没有试跑。 */
function NonScriptVersionPanel({
  appKey,
  selected,
  versions,
  loading
}: {
  appKey: string;
  selected: AppFunction;
  versions: Array<{
    id: number;
    version: string;
    status: string;
    notes: string;
    sourceBytes: number;
    endpointUrl?: string;
    artifactSha256: string;
    createdAt: string;
  }>;
  loading: boolean;
}) {
  const [version, setVersion] = useState("");
  const [notes, setNotes] = useState("");
  const [endpointUrl, setEndpointUrl] = useState("");
  const [responsePublicKey, setResponsePublicKey] = useState("");
  const [wasmBase64, setWasmBase64] = useState("");
  const createVersion = useCreateVersionMutation(appKey);
  const activateVersion = useActivateVersionMutation(appKey);
  const deleteVersion = useDeleteVersionMutation(appKey);

  const isHttp = selected.runtime === "http";

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>发布版本</CardTitle>
          <CardDescription>
            {isHttp
              ? "Endpoint 仅允许 HTTPS，禁止重定向，并在实际连接时重新解析 IP 拒绝内网与元数据地址。"
              : "WASM 最大 2MB，每次调用独立实例，内存上限 16MB，不提供网络与文件系统。"}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-2">
              <Label>版本号</Label>
              <Input
                value={version}
                onChange={(event) => setVersion(event.target.value)}
                placeholder="1.0.0"
              />
            </div>
            <div className="space-y-2">
              <Label>发版说明</Label>
              <Input value={notes} onChange={(event) => setNotes(event.target.value)} />
            </div>
          </div>
          {isHttp ? (
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
                  className="min-h-24 font-mono text-xs"
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
                className="min-h-48 font-mono text-xs"
                value={wasmBase64}
                onChange={(event) => setWasmBase64(event.target.value)}
              />
            </div>
          )}
          <Button
            disabled={createVersion.isPending || !version.trim()}
            onClick={async () => {
              try {
                await createVersion.mutateAsync({
                  name: selected.name,
                  payload: {
                    version: version.trim(),
                    notes,
                    endpointUrl: isHttp ? endpointUrl : undefined,
                    responsePublicKey: isHttp ? responsePublicKey : undefined,
                    wasmBase64: isHttp ? undefined : wasmBase64
                  }
                });
                setVersion("");
                setNotes("");
                toast.success("版本已创建，激活后生效");
              } catch (error) {
                toast.error(errorMessage(error));
              }
            }}
          >
            {createVersion.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <UploadCloud className="size-4" />
            )}
            创建版本
          </Button>
        </CardContent>
      </Card>

      <VersionTable
        versions={versions}
        loading={loading}
        activeVersion={selected.activeVersion}
        onActivate={async (target) => {
          try {
            await activateVersion.mutateAsync({ name: selected.name, version: target });
            toast.success(`版本 ${target} 已激活`);
          } catch (error) {
            toast.error(errorMessage(error));
          }
        }}
        onDelete={async (target) => {
          try {
            await deleteVersion.mutateAsync({ name: selected.name, version: target });
            toast.success(`版本 ${target} 已删除`);
          } catch (error) {
            toast.error(errorMessage(error));
          }
        }}
      />
    </div>
  );
}
