"use client";

import { Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import {
  BarChart3, Database, FileText, FolderLock, Globe, Loader2, Plus, Shield, Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import { SectionHeading } from "@/components/ui/section-heading";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import {
  useAdminAppsQuery,
  useGlobalStorageConfigsQuery, useAppStorageConfigsQuery,
  useCreateGlobalStorageConfigMutation, useUpdateGlobalStorageConfigMutation,
  useDeleteGlobalStorageConfigMutation, useTestGlobalStorageConfigMutation,
  useCreateAppStorageConfigMutation, useUpdateAppStorageConfigMutation,
  useDeleteAppStorageConfigMutation, useTestAppStorageConfigMutation,
  useStorageRulesQuery, useCreateStorageRuleMutation, useDeleteStorageRuleMutation,
  useCDNConfigQuery, useUpsertCDNConfigMutation, useDeleteCDNConfigMutation,
  useStorageUsageQuery,
} from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import { StorageConfigPanel } from "@/components/storage/storage-config-panel";
import { FileManagerPanel, type StorageConfigOption } from "@/components/storage/file-manager";
import { formatBytes } from "@/components/storage/file-shared";

/* ================================================================== */
/*  主页面                                                             */
/* ================================================================== */

function StoragePageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const tab = searchParams.get("tab") || "global";

  const appsQuery = useAdminAppsQuery();
  const apps = appsQuery.data || [];
  const [pickedAppId, setPickedAppId] = useState<number | null>(null);
  const [selectedConfigId, setSelectedConfigId] = useState<number | null>(null);
  // `?app=<appKey>` 来自应用详情页的「存储」入口：跳过来要落在同一个应用上，
  // 否则作用域会悄悄换成列表里的第一个应用。
  // 回落链直接在派生里算，不用 effect 往 state 里写（那会触发级联渲染，
  // 也过不了 react-hooks/set-state-in-effect）。
  const scopedAppKey = searchParams.get("app");
  const scopedApp = scopedAppKey ? apps.find((item) => item.appKey === scopedAppKey) : undefined;
  const selectedAppId = pickedAppId ?? scopedApp?.id ?? apps[0]?.id ?? null;

  // 全局配置
  const globalQuery = useGlobalStorageConfigsQuery();
  const createGlobal = useCreateGlobalStorageConfigMutation();
  const updateGlobal = useUpdateGlobalStorageConfigMutation();
  const deleteGlobal = useDeleteGlobalStorageConfigMutation();
  const testGlobal = useTestGlobalStorageConfigMutation();
  // 应用级配置
  const appQuery = useAppStorageConfigsQuery(selectedAppId);
  const createApp = useCreateAppStorageConfigMutation();
  const updateApp = useUpdateAppStorageConfigMutation();
  const deleteApp = useDeleteAppStorageConfigMutation();
  const testApp = useTestAppStorageConfigMutation();

  const handleGlobalSave = useCallback(async (payload: Record<string, unknown>) => {
    if (payload.id) await updateGlobal.mutateAsync({ config_id: Number(payload.id), ...payload } as never);
    else await createGlobal.mutateAsync(payload as never);
  }, [createGlobal, updateGlobal]);

  const handleAppSave = useCallback(async (payload: Record<string, unknown>) => {
    if (!selectedAppId) return;
    payload.appid = selectedAppId;
    if (payload.id) await updateApp.mutateAsync({ appid: selectedAppId, config_id: Number(payload.id), ...payload } as never);
    else await createApp.mutateAsync({ appid: selectedAppId, ...payload } as never);
  }, [createApp, updateApp, selectedAppId]);

  const globalConfigs = useMemo(() => { const l = globalQuery.data; return Array.isArray(l) ? l : []; }, [globalQuery.data]);
  const appConfigs = useMemo(() => { const l = appQuery.data; return Array.isArray(l) ? l : []; }, [appQuery.data]);

  // 汇总所有存储配置（供文件管理/规则/CDN 等 Tab 使用）
  const allConfigs = useMemo(() => [...globalConfigs, ...appConfigs], [globalConfigs, appConfigs]);

  // 文件管理的存储选择器只要「显示名 + provider」，把后端两种命名
  // （config_name / configName）在这里归一化一次，下游组件不必再关心
  const configOptions = useMemo<StorageConfigOption[]>(() => allConfigs.map((item) => {
    const raw = item as Record<string, unknown>;
    const name = String(raw.config_name ?? raw.configName ?? `#${raw.id}`);
    const provider = String(raw.provider ?? "");
    return { id: Number(raw.id), label: provider ? `${name}（${provider}）` : name, provider };
  }), [allConfigs]);

  // 永久删除与回收站清理在后端都要求超管，前端据此隐藏按钮 ——
  // 让人点了才 403 是最难解释的一种交互
  const isSuperAdmin = Boolean(useAuthStore((state) => state.operator)?.isSuperAdmin);

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="存储与资源中心" />

      <Tabs value={tab} onValueChange={(v) => router.replace(`/storage?tab=${v}`, { scroll: false })} className="space-y-5">
        <TabsList className="flex-wrap">
          <TabsTrigger value="global"><Database className="size-3.5" />全局配置</TabsTrigger>
          <TabsTrigger value="app"><FolderLock className="size-3.5" />应用级配置</TabsTrigger>
          <TabsTrigger value="files"><FileText className="size-3.5" />文件管理</TabsTrigger>
          <TabsTrigger value="rules"><Shield className="size-3.5" />上传规则</TabsTrigger>
          <TabsTrigger value="cdn"><Globe className="size-3.5" />CDN 与安全</TabsTrigger>
          <TabsTrigger value="usage"><BarChart3 className="size-3.5" />用量统计</TabsTrigger>
        </TabsList>

        {/* Tab 1: 全局配置 */}
        <TabsContent value="global">
          <StorageConfigPanel configs={globalConfigs as Record<string, unknown>[]} loading={globalQuery.isLoading} scope="global"
            onSave={handleGlobalSave} onDelete={async (id) => { await deleteGlobal.mutateAsync(id); }} onTest={async (id) => { await testGlobal.mutateAsync(id); }}
            onRefetch={() => globalQuery.refetch()} isSaving={createGlobal.isPending || updateGlobal.isPending} isDeleting={deleteGlobal.isPending} isTesting={testGlobal.isPending} />
        </TabsContent>

        {/* Tab 2: 应用级配置 */}
        <TabsContent value="app">
          <div className="space-y-4">
            {apps.length > 0 && (
              <Select value={selectedAppId ? String(selectedAppId) : ""} onValueChange={(v) => setPickedAppId(Number(v))}>
                <SelectTrigger className="h-8 w-48 text-xs"><SelectValue placeholder="选择应用" /></SelectTrigger>
                <SelectContent>{apps.map((a) => <SelectItem key={a.id} value={String(a.id)}>{a.name} ({a.id})</SelectItem>)}</SelectContent>
              </Select>
            )}
            <StorageConfigPanel configs={appConfigs as Record<string, unknown>[]} loading={appQuery.isLoading} scope="app" appId={selectedAppId}
              onSave={handleAppSave} onDelete={async (id) => { await deleteApp.mutateAsync({ configId: id, appid: selectedAppId! }); }}
              onTest={async (id) => { await testApp.mutateAsync({ configId: id, appid: selectedAppId! }); }}
              onRefetch={() => appQuery.refetch()} isSaving={createApp.isPending || updateApp.isPending} isDeleting={deleteApp.isPending} isTesting={testApp.isPending} />
          </div>
        </TabsContent>

        {/* Tab 3: 文件管理 */}
        <TabsContent value="files"><FileManagerPanel configs={configOptions} canPurge={isSuperAdmin} /></TabsContent>

        {/* Tab 4: 上传规则 */}
        <TabsContent value="rules"><RulesPanel /></TabsContent>

        {/* Tab 5: CDN 与安全 */}
        <TabsContent value="cdn"><CDNPanel allConfigs={allConfigs} selectedConfigId={selectedConfigId} onSelectConfig={setSelectedConfigId} /></TabsContent>

        {/* Tab 6: 用量统计 */}
        <TabsContent value="usage"><UsagePanel /></TabsContent>
      </Tabs>
    </div>
  );
}

export default function StoragePage() {
  return <Suspense><StoragePageInner /></Suspense>;
}

/* ================================================================== */
/*  上传规则面板                                                       */
/* ================================================================== */

function RulesPanel() {
  const rulesQuery = useStorageRulesQuery();
  const createMut = useCreateStorageRuleMutation();
  const deleteMut = useDeleteStorageRuleMutation();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [ruleType, setRuleType] = useState("upload_limit");

  const rules = rulesQuery.data || [];
  const typeLabels: Record<string, string> = { upload_limit: "上传限制", file_type: "文件类型", path_pattern: "路径模式", quota: "存储配额" };

  return (
    <Card><CardContent className="p-4 space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold flex items-center gap-2"><Shield className="size-4" />上传规则</h3>
        <Button size="sm" onClick={() => setCreating(!creating)}><Plus className="size-3.5" />新建规则</Button>
      </div>
      {creating && (
        <div className="grid gap-2 sm:grid-cols-2 rounded-lg border p-3">
          <div className="space-y-1"><Label className="text-xs">名称</Label><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="规则名称" className="h-8 text-xs" /></div>
          <div className="space-y-1"><Label className="text-xs">类型</Label>
            <Select value={ruleType} onValueChange={setRuleType}><SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>{Object.entries(typeLabels).map(([k, v]) => <SelectItem key={k} value={k}>{v}</SelectItem>)}</SelectContent>
            </Select>
          </div>
          <div className="sm:col-span-2 flex gap-1.5">
            <Button size="sm" className="flex-1" disabled={createMut.isPending} onClick={async () => {
              if (!name.trim()) { toast.error("请填写名称"); return; }
              try { await createMut.mutateAsync({ name, ruleType, ruleData: {} }); toast.success("规则已创建"); setCreating(false); setName(""); }
              catch (e) { toast.error(e instanceof ApiError ? e.message : "创建失败"); }
            }}>创建</Button>
            <Button size="sm" variant="ghost" onClick={() => setCreating(false)}>取消</Button>
          </div>
        </div>
      )}
      {rules.length === 0 ? <EmptyState title="暂无规则" description="创建上传规则以限制文件大小、类型等" /> : (
        <div className="space-y-2">{rules.map((r) => (
          <div key={r.id} className="flex items-center gap-3 rounded-lg border px-3 py-2 group">
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-1.5"><span className="text-sm font-medium">{r.name}</span>
                <Badge variant="outline" className="text-[9px]">{typeLabels[r.ruleType] || r.ruleType}</Badge>
                <Badge variant={r.isActive ? "success" : "outline"} className="text-[9px]">{r.isActive ? "启用" : "停用"}</Badge>
              </div>
            </div>
            <Button variant="ghost" size="sm" className="h-6 px-1.5 hidden group-hover:flex text-destructive" onClick={async () => {
              try { await deleteMut.mutateAsync(r.id); toast.success("已删除"); } catch (e) { toast.error(e instanceof ApiError ? e.message : "删除失败"); }
            }}><Trash2 className="size-3" /></Button>
          </div>
        ))}</div>
      )}
    </CardContent></Card>
  );
}

/* ================================================================== */
/*  CDN 与安全面板                                                     */
/* ================================================================== */

function CDNPanel({ allConfigs, selectedConfigId, onSelectConfig }: {
  allConfigs: Array<Record<string, unknown>>; selectedConfigId: number | null; onSelectConfig: (id: number | null) => void;
}) {
  const cdnQuery = useCDNConfigQuery(selectedConfigId);
  const upsertMut = useUpsertCDNConfigMutation();
  const deleteMut = useDeleteCDNConfigMutation();
  const [domain, setDomain] = useState("");
  const [protocol, setProtocol] = useState("https");
  const [cacheMaxAge, setCacheMaxAge] = useState("86400");
  const [signEnabled, setSignEnabled] = useState(false);
  const [signSecret, setSignSecret] = useState("");
  const [signTtl, setSignTtl] = useState("3600");
  const [refWhitelist, setRefWhitelist] = useState("");
  const [refBlacklist, setRefBlacklist] = useState("");
  const [ipWhitelist, setIpWhitelist] = useState("");

  useEffect(() => {
    const d = cdnQuery.data;
    if (d) {
      setDomain(d.cdnDomain || "");
      setProtocol(d.cdnProtocol || "https");
      setCacheMaxAge(String(d.cacheMaxAge || 86400));
      setSignEnabled(d.signUrlEnabled || false);
      setSignSecret(d.signUrlSecret || "");
      setSignTtl(String(d.signUrlTtl || 3600));
      setRefWhitelist((d.refererWhitelist || []).join("\n"));
      setRefBlacklist((d.refererBlacklist || []).join("\n"));
      setIpWhitelist((d.ipWhitelist || []).join("\n"));
    } else {
      setDomain(""); setProtocol("https"); setCacheMaxAge("86400");
      setSignEnabled(false); setSignSecret(""); setSignTtl("3600");
      setRefWhitelist(""); setRefBlacklist(""); setIpWhitelist("");
    }
  }, [cdnQuery.data]);

  const handleSave = async () => {
    if (!selectedConfigId) return;
    try {
      await upsertMut.mutateAsync({ configId: selectedConfigId, data: {
        cdnDomain: domain.trim(), cdnProtocol: protocol,
        cacheMaxAge: parseInt(cacheMaxAge) || 86400,
        refererWhitelist: refWhitelist.split("\n").map((s) => s.trim()).filter(Boolean),
        refererBlacklist: refBlacklist.split("\n").map((s) => s.trim()).filter(Boolean),
        ipWhitelist: ipWhitelist.split("\n").map((s) => s.trim()).filter(Boolean),
        signUrlEnabled: signEnabled,
        signUrlSecret: signSecret,
        signUrlTtl: parseInt(signTtl) || 3600,
      }});
      toast.success("CDN 配置已保存");
    } catch (e) { toast.error(e instanceof ApiError ? e.message : "保存失败"); }
  };

  const handleDelete = async () => {
    if (!selectedConfigId) return;
    try { await deleteMut.mutateAsync(selectedConfigId); toast.success("CDN 配置已删除"); }
    catch (e) { toast.error(e instanceof ApiError ? e.message : "删除失败"); }
  };

  return (
    <Card><CardContent className="p-4 space-y-4">
      <h3 className="text-sm font-semibold flex items-center gap-2"><Globe className="size-4" />CDN 与防盗链</h3>

      <Select value={selectedConfigId ? String(selectedConfigId) : ""} onValueChange={(v) => onSelectConfig(Number(v) || null)}>
        <SelectTrigger className="h-8 w-72 text-xs"><SelectValue placeholder="选择存储配置" /></SelectTrigger>
        <SelectContent>{allConfigs.map((c) => <SelectItem key={String(c.id)} value={String(c.id)}>{String(c.config_name || c.configName)} ({String(c.provider)})</SelectItem>)}</SelectContent>
      </Select>

      {selectedConfigId && (
        <div className="grid gap-4 lg:grid-cols-2">
          {/* 左列：CDN 基础 */}
          <div className="space-y-3 rounded-lg border p-3">
            <h4 className="text-xs font-semibold text-muted-foreground">CDN 基础</h4>
            <div className="space-y-1.5"><Label className="text-xs">CDN 域名</Label><Input value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="cdn.example.com" className="h-8 text-xs" /></div>
            <div className="grid grid-cols-2 gap-2">
              <div className="space-y-1.5"><Label className="text-xs">协议</Label>
                <Select value={protocol} onValueChange={setProtocol}><SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectItem value="https">HTTPS</SelectItem><SelectItem value="http">HTTP</SelectItem></SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5"><Label className="text-xs">缓存时长（秒）</Label><Input value={cacheMaxAge} onChange={(e) => setCacheMaxAge(e.target.value.replace(/[^\d]/g, ""))} className="h-8 text-xs font-mono" /></div>
            </div>
          </div>

          {/* 右列：签名 URL */}
          <div className="space-y-3 rounded-lg border p-3">
            <h4 className="text-xs font-semibold text-muted-foreground">签名 URL</h4>
            <div className="flex items-center gap-2 text-xs"><Switch checked={signEnabled} onCheckedChange={setSignEnabled} /><span>启用签名 URL（私有文件防盗链）</span></div>
            {signEnabled && (<>
              <div className="space-y-1.5"><Label className="text-xs">签名密钥</Label><Input type="password" value={signSecret} onChange={(e) => setSignSecret(e.target.value)} placeholder="签名密钥" className="h-8 text-xs font-mono" /></div>
              <div className="space-y-1.5"><Label className="text-xs">签名有效期（秒）</Label><Input value={signTtl} onChange={(e) => setSignTtl(e.target.value.replace(/[^\d]/g, ""))} className="h-8 text-xs font-mono" /></div>
            </>)}
          </div>

          {/* Referer 白名单 */}
          <div className="space-y-1.5">
            <Label className="text-xs">Referer 白名单（每行一个域名）</Label>
            <Textarea value={refWhitelist} onChange={(e) => setRefWhitelist(e.target.value)} rows={3} className="text-xs font-mono" placeholder="example.com&#10;*.example.com" />
          </div>

          {/* Referer 黑名单 */}
          <div className="space-y-1.5">
            <Label className="text-xs">Referer 黑名单（每行一个域名）</Label>
            <Textarea value={refBlacklist} onChange={(e) => setRefBlacklist(e.target.value)} rows={3} className="text-xs font-mono" placeholder="bad-site.com" />
          </div>

          {/* IP 白名单 */}
          <div className="space-y-1.5 lg:col-span-2">
            <Label className="text-xs">IP 白名单（每行一个 IP 或 CIDR）</Label>
            <Textarea value={ipWhitelist} onChange={(e) => setIpWhitelist(e.target.value)} rows={2} className="text-xs font-mono" placeholder="10.0.0.0/8&#10;192.168.1.100" />
          </div>

          {/* 操作按钮 */}
          <div className="lg:col-span-2 flex gap-2">
            <Button size="sm" disabled={upsertMut.isPending} onClick={handleSave}>
              {upsertMut.isPending ? <Loader2 className="size-3 animate-spin" /> : null} 保存配置
            </Button>
            <Button size="sm" variant="outline" disabled={deleteMut.isPending} onClick={handleDelete}>
              {deleteMut.isPending ? <Loader2 className="size-3 animate-spin" /> : <Trash2 className="size-3" />} 删除配置
            </Button>
          </div>
        </div>
      )}
    </CardContent></Card>
  );
}

/* ================================================================== */
/*  用量统计面板                                                       */
/* ================================================================== */

function UsagePanel() {
  const usageQuery = useStorageUsageQuery();
  const stats = usageQuery.data;

  return (
    <Card><CardContent className="p-4 space-y-4">
      <h3 className="text-sm font-semibold flex items-center gap-2"><BarChart3 className="size-4" />存储用量</h3>
      {!stats ? <EmptyState title="暂无统计" description="上传文件后将显示用量数据" /> : (
        <div className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-4">
            <StatCard label="文件总数" value={String(stats.totalFiles)} />
            <StatCard label="总大小" value={formatBytes(stats.totalSize)} />
            <StatCard label="活跃文件" value={String(stats.activeFiles)} />
            <StatCard label="已删除" value={String(stats.deletedFiles)} />
          </div>
          {stats.topTypes?.length > 0 && (
            <div>
              <h4 className="text-xs font-medium text-muted-foreground mb-2">文件类型分布</h4>
              <div className="space-y-1.5">
                {stats.topTypes.map((t) => (
                  <div key={t.contentType} className="flex items-center gap-3 text-xs">
                    <span className="w-32 truncate font-mono">{t.contentType}</span>
                    <div className="flex-1 h-2 rounded-full bg-muted overflow-hidden">
                      <div className="h-full bg-primary/60 rounded-full" style={{ width: `${Math.min(100, (t.count / stats.totalFiles) * 100)}%` }} />
                    </div>
                    <span className="text-muted-foreground w-16 text-right">{t.count} 个</span>
                    <span className="text-muted-foreground w-16 text-right">{formatBytes(t.size)}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </CardContent></Card>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border bg-card px-4 py-3">
      <div className="text-[10px] font-medium uppercase tracking-widest text-muted-foreground">{label}</div>
      <div className="text-lg font-semibold font-mono">{value}</div>
    </div>
  );
}
