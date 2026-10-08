"use client";

import { useCallback, useMemo, useState } from "react";
import {
  AlertTriangle,
  Copy,
  KeyRound,
  Loader2,
  MonitorSmartphone,
  Pencil,
  Plus,
  RotateCcw,
  Save,
  Server,
  ShieldCheck,
  Trash2
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type {
  OAuth2AccessTokenStrategy,
  OAuth2Client,
  OAuth2ClientPayload,
  OAuth2SubjectType,
  OAuth2TokenAuthMethod
} from "@/lib/api/oauth2-server";
import {
  useDeleteOAuth2ClientMutation,
  useOAuth2ClientsQuery,
  useOAuth2OverviewQuery,
  useRotateOAuth2SecretMutation,
  useSaveOAuth2ClientMutation
} from "@/lib/oauth2-server-hooks";
import { FieldGroup, ModeCard, SectionCard, StatusDot, SwitchRow } from "@/components/apps/app-config-primitives";
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
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup } from "@/components/ui/radio-group";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";

/**
 * 应用详情 › OAuth2 授权服务。
 *
 * 让别的网站与应用「使用本应用的账号登录」：协议由 Ory Hydra 承载，登录与授权页由 Aegis 提供。
 * 这里管理本应用的客户端；用户授权过哪些客户端在用户详情里看。见 docs/oauth2-provider.md。
 */

const GRANT_OPTIONS: Array<{ value: string; label: string; hint: string }> = [
  { value: "authorization_code", label: "授权码", hint: "网页、移动与桌面应用登录" },
  { value: "refresh_token", label: "刷新令牌", hint: "需同时申请 offline_access 权限" },
  { value: "client_credentials", label: "客户端凭据", hint: "服务端之间调用，没有用户参与" },
  { value: "urn:ietf:params:oauth:grant-type:device_code", label: "设备码", hint: "电视、命令行等不便输入的设备" }
];

const STANDARD_SCOPES = ["openid", "offline_access", "profile", "email", "phone"];

const ENDPOINT_LABELS: Array<[string, string]> = [
  ["discovery", "发现文档"],
  ["authorization", "授权端点"],
  ["token", "令牌端点"],
  ["userinfo", "用户信息"],
  ["jwks", "公钥"],
  ["revocation", "令牌吊销"],
  ["endSession", "登出"],
  ["device", "设备码"]
];

type Draft = {
  name: string;
  clientType: "confidential" | "public";
  authMethod: Exclude<OAuth2TokenAuthMethod, "none">;
  grantTypes: string[];
  redirectUris: string;
  postLogoutRedirectUris: string;
  scopes: string[];
  customScopes: string;
  audience: string;
  allowedCorsOrigins: string;
  logoUri: string;
  clientUri: string;
  policyUri: string;
  tosUri: string;
  skipConsent: boolean;
  skipLogoutConsent: boolean;
  subjectType: OAuth2SubjectType;
  accessTokenStrategy: OAuth2AccessTokenStrategy;
  frontchannelLogoutUri: string;
  backchannelLogoutUri: string;
};

const emptyDraft: Draft = {
  name: "",
  clientType: "confidential",
  authMethod: "client_secret_basic",
  grantTypes: ["authorization_code", "refresh_token"],
  redirectUris: "",
  postLogoutRedirectUris: "",
  scopes: ["openid", "offline_access", "profile"],
  customScopes: "",
  audience: "",
  allowedCorsOrigins: "",
  logoUri: "",
  clientUri: "",
  policyUri: "",
  tosUri: "",
  skipConsent: false,
  skipLogoutConsent: false,
  subjectType: "public",
  accessTokenStrategy: "",
  frontchannelLogoutUri: "",
  backchannelLogoutUri: ""
};

function draftFromClient(client: OAuth2Client): Draft {
  return {
    name: client.name,
    clientType: client.public ? "public" : "confidential",
    authMethod: client.tokenEndpointAuthMethod === "client_secret_post" ? "client_secret_post" : "client_secret_basic",
    grantTypes: client.grantTypes,
    redirectUris: client.redirectUris.join("\n"),
    postLogoutRedirectUris: client.postLogoutRedirectUris.join("\n"),
    scopes: client.scopes.filter((scope) => STANDARD_SCOPES.includes(scope)),
    customScopes: client.scopes.filter((scope) => !STANDARD_SCOPES.includes(scope)).join(" "),
    audience: client.audience.join("\n"),
    allowedCorsOrigins: client.allowedCorsOrigins.join("\n"),
    logoUri: client.logoUri ?? "",
    clientUri: client.clientUri ?? "",
    policyUri: client.policyUri ?? "",
    tosUri: client.tosUri ?? "",
    skipConsent: client.skipConsent,
    skipLogoutConsent: client.skipLogoutConsent,
    subjectType: client.subjectType,
    accessTokenStrategy: client.accessTokenStrategy ?? "",
    frontchannelLogoutUri: client.frontchannelLogoutUri ?? "",
    backchannelLogoutUri: client.backchannelLogoutUri ?? ""
  };
}

const lines = (value: string) =>
  value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);

function toPayload(draft: Draft): OAuth2ClientPayload {
  const custom = draft.customScopes.split(/[\s,]+/).map((item) => item.trim()).filter(Boolean);
  const grantTypes =
    draft.clientType === "public"
      ? draft.grantTypes.filter((grant) => grant !== "client_credentials")
      : draft.grantTypes;
  return {
    name: draft.name.trim(),
    redirectUris: lines(draft.redirectUris),
    postLogoutRedirectUris: lines(draft.postLogoutRedirectUris),
    grantTypes,
    scopes: Array.from(new Set([...draft.scopes, ...custom])),
    audience: lines(draft.audience),
    tokenEndpointAuthMethod: draft.clientType === "public" ? "none" : draft.authMethod,
    logoUri: draft.logoUri.trim(),
    clientUri: draft.clientUri.trim(),
    policyUri: draft.policyUri.trim(),
    tosUri: draft.tosUri.trim(),
    allowedCorsOrigins: lines(draft.allowedCorsOrigins),
    skipConsent: draft.skipConsent,
    skipLogoutConsent: draft.skipLogoutConsent,
    subjectType: draft.subjectType,
    accessTokenStrategy: draft.accessTokenStrategy,
    frontchannelLogoutUri: draft.frontchannelLogoutUri.trim(),
    backchannelLogoutUri: draft.backchannelLogoutUri.trim()
  };
}

function errorMessage(err: unknown, fallback: string) {
  return err instanceof ApiError ? err.message : fallback;
}

async function copyText(value: string, message: string) {
  try {
    await navigator.clipboard.writeText(value);
    toast.success(message);
  } catch {
    toast.error("复制失败");
  }
}

function grantLabel(value: string) {
  return GRANT_OPTIONS.find((item) => item.value === value)?.label ?? value;
}

export function AppOAuth2ServerPanel({ appKey }: { appKey: string }) {
  const overviewQuery = useOAuth2OverviewQuery(appKey);
  const overview = overviewQuery.data;
  const usable = Boolean(overview?.enabled && overview.ready);
  const clientsQuery = useOAuth2ClientsQuery(appKey, usable);
  const clients = useMemo(() => clientsQuery.data?.items ?? [], [clientsQuery.data]);

  const saveMutation = useSaveOAuth2ClientMutation(appKey);
  const rotateMutation = useRotateOAuth2SecretMutation(appKey);
  const deleteMutation = useDeleteOAuth2ClientMutation(appKey);

  const [editorOpen, setEditorOpen] = useState(false);
  // 每次打开自增，作为编辑器的 key：本地状态随开启重置，关闭动画不受影响
  const [editorSession, setEditorSession] = useState(0);
  const [editing, setEditing] = useState<OAuth2Client | null>(null);
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [revealed, setRevealed] = useState<OAuth2Client | null>(null);
  const [pendingRotate, setPendingRotate] = useState<OAuth2Client | null>(null);
  const [pendingDelete, setPendingDelete] = useState<OAuth2Client | null>(null);

  const openCreate = useCallback(() => {
    setEditing(null);
    setDraft(emptyDraft);
    setEditorSession((prev) => prev + 1);
    setEditorOpen(true);
  }, []);

  const openEdit = useCallback((client: OAuth2Client) => {
    setEditing(client);
    setDraft(draftFromClient(client));
    setEditorSession((prev) => prev + 1);
    setEditorOpen(true);
  }, []);

  const handleSave = useCallback(async () => {
    const payload = toPayload(draft);
    if (!payload.name) {
      toast.error("请填写客户端名称");
      return;
    }
    try {
      const saved = await saveMutation.mutateAsync({ clientId: editing?.clientId, payload });
      setEditorOpen(false);
      if (!editing) {
        // 新建：密钥只此一次以明文返回，必须当场展示
        setRevealed(saved);
      } else {
        toast.success("客户端已保存");
      }
    } catch (err) {
      toast.error(errorMessage(err, "保存失败"));
    }
  }, [draft, editing, saveMutation]);

  const handleRotate = useCallback(async () => {
    if (!pendingRotate) return;
    try {
      const rotated = await rotateMutation.mutateAsync(pendingRotate.clientId);
      setPendingRotate(null);
      setRevealed(rotated);
    } catch (err) {
      toast.error(errorMessage(err, "重新生成失败"));
    }
  }, [pendingRotate, rotateMutation]);

  const handleDelete = useCallback(async () => {
    if (!pendingDelete) return;
    try {
      await deleteMutation.mutateAsync(pendingDelete.clientId);
      toast.success(`客户端「${pendingDelete.name}」已删除`);
      setPendingDelete(null);
    } catch (err) {
      toast.error(errorMessage(err, "删除失败"));
    }
  }, [deleteMutation, pendingDelete]);

  return (
    <div className="space-y-4">
      <OverviewCard loading={overviewQuery.isLoading} overview={overview} />

      {usable && (
        <SectionCard
          icon={<KeyRound className="size-4" />}
          title="客户端"
          description="每个接入方一个客户端，令牌与授权记录按客户端隔离"
          aside={
            <Button size="sm" className="h-8 gap-1 text-xs" onClick={openCreate}>
              <Plus className="size-3" />
              新建客户端
            </Button>
          }
        >
          {clientsQuery.isLoading ? (
            <div className="space-y-2">
              <Skeleton className="h-24 w-full rounded-xl" />
              <Skeleton className="h-24 w-full rounded-xl" />
            </div>
          ) : clients.length === 0 ? (
            <div className="rounded-xl border border-dashed py-10 text-center">
              <KeyRound className="mx-auto size-7 text-muted-foreground/60" />
              <div className="mt-3 text-sm font-medium">暂无客户端</div>
              <p className="mt-1 text-xs text-muted-foreground">新建客户端后，接入方即可使用本应用的账号登录</p>
              <Button size="sm" className="mt-4 h-8 gap-1 text-xs" onClick={openCreate}>
                <Plus className="size-3" />
                新建客户端
              </Button>
            </div>
          ) : (
            <div className="space-y-2">
              {clients.map((client) => (
                <ClientRow
                  key={client.clientId}
                  client={client}
                  onEdit={() => openEdit(client)}
                  onRotate={() => setPendingRotate(client)}
                  onDelete={() => setPendingDelete(client)}
                />
              ))}
            </div>
          )}
        </SectionCard>
      )}

      <ClientEditor
        key={editorSession}
        open={editorOpen}
        onOpenChange={setEditorOpen}
        editing={editing}
        draft={draft}
        setDraft={setDraft}
        saving={saveMutation.isPending}
        onSave={handleSave}
      />

      <SecretDialog client={revealed} onClose={() => setRevealed(null)} />

      <AlertDialog open={Boolean(pendingRotate)} onOpenChange={(open) => !open && setPendingRotate(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>重新生成「{pendingRotate?.name}」的密钥？</AlertDialogTitle>
            <AlertDialogDescription>
              旧密钥立即失效，该客户端已签发的访问令牌一并吊销。接入方需要换上新密钥后才能继续换取令牌。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleRotate} disabled={rotateMutation.isPending}>
              {rotateMutation.isPending ? "生成中..." : "重新生成"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除客户端「{pendingDelete?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>
              接入方将无法再发起登录，用户对它的授权记录与已签发的令牌全部失效。此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? "删除中..." : "确认删除"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// ── 概览 ────────────────────────────────────────────────────────────

function OverviewCard({
  loading,
  overview
}: {
  loading: boolean;
  overview?: ReturnType<typeof useOAuth2OverviewQuery>["data"];
}) {
  if (loading || !overview) {
    return (
      <SectionCard icon={<Server className="size-4" />} title="授权服务" description="正在读取授权服务状态">
        <div className="space-y-2">
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-4 w-1/2" />
          <Skeleton className="h-4 w-3/5" />
        </div>
      </SectionCard>
    );
  }

  const status = !overview.enabled ? (
    <StatusDot active={false} labelActive="" labelInactive="未启用" />
  ) : overview.ready ? (
    <StatusDot active labelActive="运行中" labelInactive="" />
  ) : (
    <Badge variant="danger">无法连接</Badge>
  );

  return (
    <SectionCard
      icon={<Server className="size-4" />}
      title="授权服务"
      description="标准 OAuth 2.0 与 OpenID Connect，接入方按发现文档即可完成配置"
      aside={status}
    >
      {!overview.enabled ? (
        <Notice>
          授权服务尚未启用。部署方需在服务端配置中开启 OAUTH2_SERVER_ENABLED，并以 oauth2 配置档启动 Hydra。
        </Notice>
      ) : !overview.ready ? (
        <Notice>{overview.readyError ?? "无法连接授权服务"}，请检查 HYDRA_ADMIN_URL 与 Hydra 运行状态。</Notice>
      ) : (
        <div className="space-y-5">
          <FieldGroup label="签发方" hint={`已有 ${overview.clientCount} 个客户端`}>
            <CopyLine value={overview.issuer ?? ""} label="签发方" />
          </FieldGroup>
          {overview.endpoints && (
            <FieldGroup label="端点">
              <div className="divide-y divide-border rounded-xl bg-muted/50">
                {ENDPOINT_LABELS.filter(([key]) => overview.endpoints?.[key]).map(([key, label]) => (
                  <div key={key} className="flex items-center gap-3 px-3 py-2">
                    <span className="w-16 shrink-0 text-xs text-muted-foreground">{label}</span>
                    <code className="min-w-0 flex-1 truncate font-mono text-[11px]">{overview.endpoints?.[key]}</code>
                    <Button
                      size="icon"
                      variant="ghost"
                      className="size-7 shrink-0"
                      aria-label={`复制${label}`}
                      onClick={() => void copyText(overview.endpoints?.[key] ?? "", `${label}已复制`)}
                    >
                      <Copy className="size-3.5" />
                    </Button>
                  </div>
                ))}
              </div>
            </FieldGroup>
          )}
          <FieldGroup label="用户标识">
            <p className="text-xs text-muted-foreground">{overview.subjectClaim}</p>
          </FieldGroup>
        </div>
      )}
    </SectionCard>
  );
}

function Notice({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2 rounded-xl bg-amber-500/10 px-3 py-2.5 text-xs text-amber-800 ring-1 ring-inset ring-amber-500/20 dark:text-amber-300">
      <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
      <span>{children}</span>
    </div>
  );
}

function CopyLine({ value, label }: { value: string; label: string }) {
  return (
    <div className="flex items-center gap-2 rounded-xl bg-muted px-3 py-2">
      <code className="min-w-0 flex-1 truncate font-mono text-xs">{value || "未配置"}</code>
      {value && (
        <Button
          size="icon"
          variant="ghost"
          className="size-7 shrink-0"
          aria-label={`复制${label}`}
          onClick={() => void copyText(value, `${label}已复制`)}
        >
          <Copy className="size-3.5" />
        </Button>
      )}
    </div>
  );
}

// ── 客户端列表 ──────────────────────────────────────────────────────

function ClientRow({
  client,
  onEdit,
  onRotate,
  onDelete
}: {
  client: OAuth2Client;
  onEdit: () => void;
  onRotate: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="rounded-xl border border-border p-3 sm:p-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 items-start gap-3">
          {client.logoUri ? (
            // eslint-disable-next-line @next/next/no-img-element -- 接入方自填的任意外链，不走 next/image 的域名白名单
            <img src={client.logoUri} alt="" className="size-10 shrink-0 rounded-xl object-cover ring-1 ring-border" />
          ) : (
            <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-muted text-muted-foreground">
              {client.public ? <MonitorSmartphone className="size-4" /> : <Server className="size-4" />}
            </div>
          )}
          <div className="min-w-0 space-y-1">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="truncate text-sm font-medium">{client.name}</span>
              <Badge variant="outline" size="sm">
                {client.public ? "公开客户端" : "机密客户端"}
              </Badge>
              {client.skipConsent && (
                <Badge variant="info" size="sm">
                  免授权确认
                </Badge>
              )}
            </div>
            <button
              type="button"
              className="flex max-w-full items-center gap-1 font-mono text-[11px] text-muted-foreground hover:text-foreground"
              onClick={() => void copyText(client.clientId, "客户端 ID 已复制")}
            >
              <span className="truncate">{client.clientId}</span>
              <Copy className="size-3 shrink-0" />
            </button>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <Button size="sm" variant="outline" className="h-8 gap-1 text-xs" onClick={onEdit}>
            <Pencil className="size-3" />
            编辑
          </Button>
          {!client.public && (
            <Button size="sm" variant="ghost" className="h-8 gap-1 text-xs" onClick={onRotate}>
              <RotateCcw className="size-3" />
              重新生成密钥
            </Button>
          )}
          <Button
            size="icon"
            variant="ghost"
            className="size-8 text-destructive hover:text-destructive"
            aria-label="删除客户端"
            onClick={onDelete}
          >
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </div>
      <dl className="mt-3 grid gap-x-6 gap-y-1.5 text-xs sm:grid-cols-2">
        <Meta label="授权类型" value={client.grantTypes.map(grantLabel).join("、") || "无"} />
        <Meta label="权限" value={client.scopes.join(" ") || "无"} mono />
        <Meta label="回调地址" value={client.redirectUris.join("\n") || "无"} mono />
        <Meta label="主体类型" value={client.subjectType === "pairwise" ? "按客户端区分（pairwise）" : "用户 ID（public）"} />
      </dl>
    </div>
  );
}

function Meta({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex min-w-0 gap-2">
      <dt className="w-14 shrink-0 text-muted-foreground">{label}</dt>
      <dd className={`min-w-0 whitespace-pre-line break-all ${mono ? "font-mono text-[11px]" : ""}`}>{value}</dd>
    </div>
  );
}

// ── 密钥只展示一次 ──────────────────────────────────────────────────

function SecretDialog({ client, onClose }: { client: OAuth2Client | null; onClose: () => void }) {
  return (
    <Dialog open={Boolean(client)} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{client?.clientSecret ? "保存客户端凭据" : "客户端已创建"}</DialogTitle>
          <DialogDescription>
            {client?.clientSecret
              ? "密钥只显示这一次，关闭后无法再次查看。遗失后只能重新生成。"
              : "公开客户端没有密钥，接入方须使用 PKCE。"}
          </DialogDescription>
        </DialogHeader>
        {client && (
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label className="text-xs">客户端 ID</Label>
              <CopyLine value={client.clientId} label="客户端 ID" />
            </div>
            {client.clientSecret && (
              <div className="space-y-1.5">
                <Label className="text-xs">客户端密钥</Label>
                <CopyLine value={client.clientSecret} label="客户端密钥" />
              </div>
            )}
            <div className="flex justify-end">
              <Button size="sm" className="h-8 text-xs" onClick={onClose}>
                已妥善保存
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

// ── 编辑器 ──────────────────────────────────────────────────────────

function ClientEditor({
  open,
  onOpenChange,
  editing,
  draft,
  setDraft,
  saving,
  onSave
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editing: OAuth2Client | null;
  draft: Draft;
  setDraft: React.Dispatch<React.SetStateAction<Draft>>;
  saving: boolean;
  onSave: () => void;
}) {
  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft((prev) => ({ ...prev, [key]: value }));
  const toggleIn = (key: "grantTypes" | "scopes", value: string, on: boolean) =>
    setDraft((prev) => ({
      ...prev,
      [key]: on ? Array.from(new Set([...prev[key], value])) : prev[key].filter((item) => item !== value)
    }));
  const needsRedirect = draft.grantTypes.includes("authorization_code");
  // 公开客户端不能改成机密客户端（后端同样拒绝）
  const typeLocked = Boolean(editing?.public);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-xl">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle>{editing ? `编辑「${editing.name}」` : "新建客户端"}</SheetTitle>
          <SheetDescription>OAuth2 / OIDC 客户端</SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-6 px-5 py-5">
            <FieldGroup label="基本信息">
              <EditorField label="名称" hint="展示在登录页与授权页上">
                <Input className="h-9 text-sm" value={draft.name} maxLength={64} placeholder="例如：官方网站" onChange={(e) => set("name", e.target.value)} />
              </EditorField>
              <RadioGroup
                value={draft.clientType}
                onValueChange={(value) => set("clientType", value as Draft["clientType"])}
                className="grid gap-2 sm:grid-cols-2"
              >
                <ModeCard
                  value="confidential"
                  active={draft.clientType === "confidential"}
                  title="机密客户端"
                  description="有服务端，能保管密钥"
                  icon={<Server className="size-3.5" />}
                  disabled={typeLocked}
                />
                <ModeCard
                  value="public"
                  active={draft.clientType === "public"}
                  title="公开客户端"
                  description="单页、移动与桌面应用，使用 PKCE"
                  icon={<MonitorSmartphone className="size-3.5" />}
                  disabled={Boolean(editing) && !typeLocked}
                />
              </RadioGroup>
              {draft.clientType === "confidential" && (
                <EditorField label="密钥传递方式">
                  <Select value={draft.authMethod} onValueChange={(value) => set("authMethod", value as Draft["authMethod"])}>
                    <SelectTrigger className="h-9 text-sm">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="client_secret_basic">HTTP Basic 认证头</SelectItem>
                      <SelectItem value="client_secret_post">表单参数</SelectItem>
                    </SelectContent>
                  </Select>
                </EditorField>
              )}
            </FieldGroup>

            <FieldGroup label="授权类型">
              <div className="grid gap-2">
                {GRANT_OPTIONS.filter((item) => draft.clientType === "confidential" || item.value !== "client_credentials").map((item) => (
                  <SwitchRow
                    key={item.value}
                    label={item.label}
                    hint={item.hint}
                    checked={draft.grantTypes.includes(item.value)}
                    onChange={(on) => toggleIn("grantTypes", item.value, on)}
                  />
                ))}
              </div>
            </FieldGroup>

            <FieldGroup label="地址" hint="每行一个">
              <EditorField
                label="回调地址"
                hint={needsRedirect ? "网页用 https；http 仅限本机；移动应用用 com.example.app:/callback" : "授权码模式才需要"}
              >
                <Textarea
                  className="min-h-20 font-mono text-xs"
                  value={draft.redirectUris}
                  placeholder="https://app.example.com/callback"
                  onChange={(e) => set("redirectUris", e.target.value)}
                />
              </EditorField>
              <EditorField label="登出后跳转地址" hint="可选">
                <Textarea
                  className="min-h-16 font-mono text-xs"
                  value={draft.postLogoutRedirectUris}
                  placeholder="https://app.example.com/"
                  onChange={(e) => set("postLogoutRedirectUris", e.target.value)}
                />
              </EditorField>
              {draft.clientType === "public" && (
                <EditorField label="允许跨域的来源" hint="单页应用直接调用令牌端点时需要">
                  <Textarea
                    className="min-h-16 font-mono text-xs"
                    value={draft.allowedCorsOrigins}
                    placeholder="https://app.example.com"
                    onChange={(e) => set("allowedCorsOrigins", e.target.value)}
                  />
                </EditorField>
              )}
            </FieldGroup>

            <FieldGroup label="权限">
              <div className="grid gap-2 sm:grid-cols-2">
                {STANDARD_SCOPES.map((scope) => (
                  <SwitchRow
                    key={scope}
                    label={scope}
                    hint={SCOPE_HINTS[scope]}
                    checked={draft.scopes.includes(scope)}
                    onChange={(on) => toggleIn("scopes", scope, on)}
                  />
                ))}
              </div>
              <EditorField label="自定义权限" hint="以空格分隔，供接入方自己的资源服务器使用">
                <Input className="h-9 font-mono text-xs" value={draft.customScopes} placeholder="orders:read orders:write" onChange={(e) => set("customScopes", e.target.value)} />
              </EditorField>
              <EditorField label="受众" hint="可选，每行一个资源服务器标识">
                <Textarea className="min-h-14 font-mono text-xs" value={draft.audience} placeholder="https://api.example.com" onChange={(e) => set("audience", e.target.value)} />
              </EditorField>
            </FieldGroup>

            <FieldGroup label="授权页展示" hint="可选">
              <div className="grid gap-3 sm:grid-cols-2">
                <EditorField label="Logo 地址">
                  <Input className="h-9 font-mono text-xs" value={draft.logoUri} placeholder="https://" onChange={(e) => set("logoUri", e.target.value)} />
                </EditorField>
                <EditorField label="主页地址">
                  <Input className="h-9 font-mono text-xs" value={draft.clientUri} placeholder="https://" onChange={(e) => set("clientUri", e.target.value)} />
                </EditorField>
                <EditorField label="隐私政策">
                  <Input className="h-9 font-mono text-xs" value={draft.policyUri} placeholder="https://" onChange={(e) => set("policyUri", e.target.value)} />
                </EditorField>
                <EditorField label="服务条款">
                  <Input className="h-9 font-mono text-xs" value={draft.tosUri} placeholder="https://" onChange={(e) => set("tosUri", e.target.value)} />
                </EditorField>
              </div>
            </FieldGroup>

            <FieldGroup label="高级">
              <div className="grid gap-2">
                <SwitchRow
                  label="免授权确认"
                  hint="仅限自家应用：用户登录后不再出现授权页"
                  icon={<ShieldCheck className="size-3.5" />}
                  checked={draft.skipConsent}
                  onChange={(on) => set("skipConsent", on)}
                />
                <SwitchRow
                  label="免登出确认"
                  hint="由该客户端发起的登出不再询问用户"
                  checked={draft.skipLogoutConsent}
                  onChange={(on) => set("skipLogoutConsent", on)}
                />
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <EditorField label="用户标识">
                  <Select value={draft.subjectType} onValueChange={(value) => set("subjectType", value as OAuth2SubjectType)}>
                    <SelectTrigger className="h-9 text-sm">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="public">用户 ID</SelectItem>
                      <SelectItem value="pairwise">按客户端区分</SelectItem>
                    </SelectContent>
                  </Select>
                </EditorField>
                <EditorField label="访问令牌格式">
                  <Select
                    value={draft.accessTokenStrategy || "default"}
                    onValueChange={(value) => set("accessTokenStrategy", value === "default" ? "" : (value as OAuth2AccessTokenStrategy))}
                  >
                    <SelectTrigger className="h-9 text-sm">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="default">跟随服务默认</SelectItem>
                      <SelectItem value="opaque">不透明令牌</SelectItem>
                      <SelectItem value="jwt">JWT</SelectItem>
                    </SelectContent>
                  </Select>
                </EditorField>
                <EditorField label="前端通道登出地址" hint="可选">
                  <Input className="h-9 font-mono text-xs" value={draft.frontchannelLogoutUri} placeholder="https://" onChange={(e) => set("frontchannelLogoutUri", e.target.value)} />
                </EditorField>
                <EditorField label="后端通道登出地址" hint="可选">
                  <Input className="h-9 font-mono text-xs" value={draft.backchannelLogoutUri} placeholder="https://" onChange={(e) => set("backchannelLogoutUri", e.target.value)} />
                </EditorField>
              </div>
            </FieldGroup>
          </div>
        </ScrollArea>

        <div className="flex items-center justify-end gap-2 border-t px-5 py-3">
          <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" className="h-8 gap-1 text-xs" disabled={saving} onClick={onSave}>
            {saving ? <Loader2 className="size-3 animate-spin" /> : <Save className="size-3" />}
            {saving ? "保存中..." : editing ? "保存" : "创建"}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

const SCOPE_HINTS: Record<string, string> = {
  openid: "必选，用户标识",
  offline_access: "签发刷新令牌",
  profile: "昵称、账号名与头像",
  email: "邮箱地址",
  phone: "手机号码"
};

function EditorField({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-xs font-medium">{label}</Label>
        {hint && <span className="text-[11px] text-muted-foreground">{hint}</span>}
      </div>
      {children}
    </div>
  );
}
