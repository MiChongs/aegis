"use client";

import { useEffect, useMemo, useState } from "react";
import { Copy, Download, EyeOff, Fingerprint, KeyRound, LockKeyhole, QrCode, RefreshCw, ShieldCheck, Smartphone, Trash2 } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import {
  useAdminPasskeysQuery, useAdminRecoveryCodesQuery, useAdminSecurityStatusQuery, useAdminSessionQuery,
  useBeginAdminPasskeyRegistrationMutation, useBeginAdminTOTPEnrollmentMutation,
  useDeleteAdminPasskeyMutation, useDisableAdminTOTPMutation, useEnableAdminTOTPMutation,
  useFinishAdminPasskeyRegistrationMutation, useGenerateAdminRecoveryCodesMutation, useRegenerateAdminRecoveryCodesMutation,
} from "@/lib/admin-hooks";
import type { RecoveryCodeIssueResult, TOTPEnrollment } from "@/lib/api/types";
import { createPasskeyRegistrationCredential, passkeyRegistrationSupported, passkeySecureContextIssue } from "@/lib/webauthn";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";

// ── 工具 ──

const RECOVERY_KEY = "aegis.admin.recovery-codes";
type CodeBundle = { generatedAt: string; codes: string[] };

function fmtDate(v?: string | null) {
  if (!v) return "—";
  const d = new Date(v);
  return isNaN(d.getTime()) ? "—" : d.toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function modReady(mods: { key: string; enabled: boolean; ready: boolean }[] | undefined, key: string) {
  const m = mods?.find(e => e.key === key);
  return Boolean(m?.enabled && m?.ready);
}

function saveBundle(b: CodeBundle | null) {
  if (typeof window === "undefined") return;
  b ? sessionStorage.setItem(RECOVERY_KEY, JSON.stringify(b)) : sessionStorage.removeItem(RECOVERY_KEY);
}

function toBundle(r?: RecoveryCodeIssueResult | null): CodeBundle | null {
  return r?.codes?.length ? { generatedAt: r.generatedAt, codes: r.codes } : null;
}

function dlCodes(b: CodeBundle) {
  const text = ["Aegis 管理员恢复码", `生成时间: ${fmtDate(b.generatedAt)}`, "", ...b.codes].join("\n");
  const a = document.createElement("a");
  a.href = URL.createObjectURL(new Blob([text], { type: "text/plain;charset=utf-8" }));
  a.download = `aegis-recovery-codes-${b.generatedAt.replace(/[:T]/g, "-").slice(0, 16)}.txt`;
  document.body.appendChild(a); a.click(); a.remove(); URL.revokeObjectURL(a.href);
}

// ── 主组件 ──

export function AccountSecurityPanel() {
  const sessionQ = useAdminSessionQuery();
  const fallback = Boolean(sessionQ.data?.fallbackToken);
  const statusQ = useAdminSecurityStatusQuery(!fallback);
  const recoveryQ = useAdminRecoveryCodesQuery(!fallback);
  const passkeysQ = useAdminPasskeysQuery(!fallback);

  const beginEnroll = useBeginAdminTOTPEnrollmentMutation();
  const enableTOTP = useEnableAdminTOTPMutation();
  const disableTOTP = useDisableAdminTOTPMutation();
  const genCodes = useGenerateAdminRecoveryCodesMutation();
  const regenCodes = useRegenerateAdminRecoveryCodesMutation();
  const beginPasskey = useBeginAdminPasskeyRegistrationMutation();
  const finishPasskey = useFinishAdminPasskeyRegistrationMutation();
  const deletePasskey = useDeleteAdminPasskeyMutation();

  const [enrollment, setEnrollment] = useState<TOTPEnrollment | null>(null);
  const [totpCode, setTotpCode] = useState("");
  const [disableCode, setDisableCode] = useState("");
  const [disableRecovery, setDisableRecovery] = useState("");
  const [recoveryCode, setRecoveryCode] = useState("");
  const [recoveryFallback, setRecoveryFallback] = useState("");
  const [passkeyName, setPasskeyName] = useState("");
  const [plainCodes, setPlainCodes] = useState<CodeBundle | null>(null);

  useEffect(() => {
    if (typeof window === "undefined") return;
    try {
      const raw = sessionStorage.getItem(RECOVERY_KEY);
      if (raw) { const p = JSON.parse(raw); if (Array.isArray(p.codes)) setPlainCodes(p); }
    } catch { sessionStorage.removeItem(RECOVERY_KEY); }
  }, []);

  const status = statusQ.data;
  const recoverySummary = recoveryQ.data || status?.recoveryCodes;
  const passkeySummary = passkeysQ.data || status?.passkeys;
  const mods = status?.modules;
  const totpOk = modReady(mods, "totp");
  const recoveryOk = modReady(mods, "recovery_codes");
  const passkeyOk = modReady(mods, "passkey");
  const passkeySupport = useMemo(() => passkeyRegistrationSupported(), []);

  // ── 处理函数 ──

  async function handleBeginEnroll() {
    try { const r = await beginEnroll.mutateAsync(); setEnrollment(r); setTotpCode(""); toast.success("已生成配置"); }
    catch (e) { toast.error(e instanceof ApiError ? e.message : "生成失败"); }
  }

  async function handleEnableTOTP() {
    if (!enrollment || !totpCode.trim()) return;
    try {
      const r = await enableTOTP.mutateAsync({ enrollmentId: enrollment.enrollmentId, code: totpCode.trim() });
      const b = toBundle(r.recoveryCodes); setEnrollment(null); setTotpCode(""); setDisableCode(""); setDisableRecovery("");
      setRecoveryCode(""); setRecoveryFallback(""); setPlainCodes(b); saveBundle(b);
      toast.success("双因子已启用");
    } catch (e) { toast.error(e instanceof ApiError ? e.message : "启用失败"); }
  }

  async function handleDisableTOTP() {
    try {
      await disableTOTP.mutateAsync({ code: disableCode.trim() || undefined, recoveryCode: disableRecovery.trim() || undefined });
      setEnrollment(null); setTotpCode(""); setDisableCode(""); setDisableRecovery("");
      setPlainCodes(null); saveBundle(null); toast.success("双因子已关闭");
    } catch (e) { toast.error(e instanceof ApiError ? e.message : "关闭失败"); }
  }

  async function handleGenCodes(regen = false) {
    try {
      const payload = { code: recoveryCode.trim() || undefined, recoveryCode: recoveryFallback.trim() || undefined };
      const r = regen ? await regenCodes.mutateAsync(payload) : await genCodes.mutateAsync(payload);
      const b = toBundle(r); setPlainCodes(b); saveBundle(b); setRecoveryCode(""); setRecoveryFallback("");
      toast.success(regen ? "恢复码已重置" : "恢复码已生成");
    } catch (e) { toast.error(e instanceof ApiError ? e.message : "生成失败"); }
  }

  async function handleRegisterPasskey() {
    // 安全上下文的问题要在点击之前就说清楚：局域网 IP 打开时 `navigator.credentials`
    // 根本不存在，只报「不支持」会让人去查浏览器版本，而真正要改的是访问地址。
    const insecure = passkeySecureContextIssue();
    if (insecure) { toast.error(insecure); return; }
    if (!passkeySupport) { toast.error("当前环境不支持 Passkey"); return; }
    try {
      const reg = await beginPasskey.mutateAsync();
      const cred = await createPasskeyRegistrationCredential(reg.options);
      await finishPasskey.mutateAsync({ challengeId: reg.session.challengeId, credential: cred, credentialName: passkeyName.trim() || undefined });
      setPasskeyName(""); toast.success("Passkey 已绑定");
    } catch (e) {
      // 后端的 40039（来源不在白名单）与浏览器的 SecurityError 都已带处置办法，原样透出
      toast.error(e instanceof ApiError ? e.message : e instanceof Error ? e.message : "绑定失败", { duration: 10000 });
    }
  }

  async function handleDeletePasskey(id: string) {
    try { await deletePasskey.mutateAsync(id); toast.success("已移除"); }
    catch (e) { toast.error(e instanceof ApiError ? e.message : "移除失败"); }
  }

  // ── 渲染 ──

  if (sessionQ.isLoading || (!fallback && statusQ.isLoading && !status)) return <LoadingState title="加载账户安全" />;
  if (fallback) return <EmptyState title="当前会话不可管理" description="静态令牌会话不支持账户安全操作。" />;
  if (!status) return <EmptyState title="账户安全不可用" description="当前管理员安全状态未返回。" />;

  const recoveryItems = recoverySummary?.items || [];
  const passkeyItems = passkeySummary?.items || [];

  return (
    <div className="space-y-6">
      {/* ── 状态概览 ── */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatusCard icon={LockKeyhole} label="双因子" value={status.twoFactorEnabled ? "已启用" : "未启用"} ok={status.twoFactorEnabled} sub={status.twoFactor.accountName || undefined} />
        <StatusCard icon={Smartphone} label="Passkey" value={status.passkeyEnabled ? `${passkeySummary?.count || 0} 个` : "未绑定"} ok={status.passkeyEnabled} />
        <StatusCard icon={KeyRound} label="恢复码" value={`${recoverySummary?.remaining || 0} / ${recoverySummary?.total || 0}`} ok={Boolean(recoverySummary?.enabled)} sub={fmtDate(recoverySummary?.generatedAt)} />
        <StatusCard icon={ShieldCheck} label="密码" value={status.hasPassword ? "已设置" : "未设置"} ok={status.hasPassword} sub={fmtDate(status.lastLoginAt)} />
      </div>

      <Separator />

      {/* ── 配置区 ── */}
      <Accordion type="multiple" defaultValue={["totp"]} className="space-y-3">

        {/* ━━ TOTP ━━ */}
        <AccordionItem value="totp" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">
            <span className="flex items-center gap-2">
              <LockKeyhole className="size-4 text-muted-foreground" />
              双因子认证 (TOTP)
              <Badge variant={status.twoFactorEnabled ? "success" : "outline"} className="ml-2">
                {status.twoFactorEnabled ? "已启用" : "未启用"}
              </Badge>
            </span>
          </AccordionTrigger>
          <AccordionContent className="pb-4">
            {!totpOk ? (
              <p className="text-sm text-muted-foreground">TOTP 模块未在平台安全中启用。</p>
            ) : !status.twoFactorEnabled ? (
              <div className="space-y-4">
                {!enrollment ? (
                  <div className="flex items-center justify-between rounded-xl border border-dashed px-4 py-6">
                    <p className="text-sm text-muted-foreground">点击开始配置双因子认证</p>
                    <Button size="sm" onClick={() => void handleBeginEnroll()} disabled={beginEnroll.isPending}>
                      {beginEnroll.isPending ? "生成中..." : "开始配置"}
                    </Button>
                  </div>
                ) : (
                  <div className="flex flex-col gap-5 sm:flex-row">
                    {/* 左侧：二维码 */}
                    <div className="flex flex-col items-center gap-2 shrink-0">
                      <div className="rounded-xl border bg-white p-3">
                        <QRCodeSVG value={enrollment.provisioningUri} size={160} level="M" />
                      </div>
                      <p className="text-[10px] text-muted-foreground">使用认证器 App 扫码</p>
                    </div>
                    {/* 右侧：密钥 + 验证 */}
                    <div className="flex-1 space-y-3">
                      <Field label="密钥（手动输入）"><Input className="h-8 text-sm font-mono" value={enrollment.secret} readOnly /></Field>
                      <Field label="验证码"><Input className="h-8 text-sm" placeholder="输入 6 位验证码" value={totpCode} onChange={e => setTotpCode(e.target.value)} /></Field>
                      <div className="flex gap-2">
                        <Button size="sm" onClick={() => void handleEnableTOTP()} disabled={enableTOTP.isPending || !totpCode.trim()}>
                          {enableTOTP.isPending ? "启用中..." : "确认启用"}
                        </Button>
                        <Button size="sm" variant="outline" onClick={() => setEnrollment(null)}>取消</Button>
                      </div>
                    </div>
                  </div>
                )}
              </div>
            ) : (
              <div className="space-y-3">
                <div className="flex items-center gap-3 text-sm text-muted-foreground">
                  <span>账号: {status.twoFactor.accountName || "—"}</span>
                  <span>最近校验: {fmtDate(status.twoFactor.lastVerifiedAt)}</span>
                </div>
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field label="验证码"><Input className="h-8 text-sm" placeholder="当前验证码" value={disableCode} onChange={e => setDisableCode(e.target.value)} /></Field>
                  <Field label="或恢复码"><Input className="h-8 text-sm" placeholder="恢复码" value={disableRecovery} onChange={e => setDisableRecovery(e.target.value)} /></Field>
                </div>
                <Button size="sm" variant="destructive" onClick={() => void handleDisableTOTP()} disabled={disableTOTP.isPending}>
                  {disableTOTP.isPending ? "关闭中..." : "关闭双因子"}
                </Button>
              </div>
            )}
          </AccordionContent>
        </AccordionItem>

        {/* ━━ 恢复码 ━━ */}
        <AccordionItem value="recovery" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">
            <span className="flex items-center gap-2">
              <KeyRound className="size-4 text-muted-foreground" />
              恢复码
              <Badge variant="outline" className="ml-2">
                {recoverySummary?.remaining || 0} / {recoverySummary?.total || 0}
              </Badge>
            </span>
          </AccordionTrigger>
          <AccordionContent className="pb-4 space-y-4">
            {!recoveryOk ? (
              <p className="text-sm text-muted-foreground">恢复码模块未在平台安全中启用。</p>
            ) : (
              <>
                {/* 已有恢复码列表 */}
                {recoveryItems.length > 0 && (
                  <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                    {recoveryItems.map(item => (
                      <div key={item.id} className="flex items-center justify-between rounded-lg border px-3 py-2">
                        <span className={`font-mono text-sm ${item.usedAt ? "text-muted-foreground line-through" : "text-foreground"}`}>{item.codeHint}</span>
                        <Badge variant={item.usedAt ? "outline" : "success"} className="text-[10px]">{item.usedAt ? "已用" : "可用"}</Badge>
                      </div>
                    ))}
                  </div>
                )}

                {/* 明文恢复码展示 */}
                {plainCodes?.codes?.length ? (
                  <div className="rounded-xl border bg-muted/30 p-4 space-y-3">
                    <div className="text-xs text-muted-foreground">生成于 {fmtDate(plainCodes.generatedAt)}</div>
                    <div className="grid gap-1.5 sm:grid-cols-2 lg:grid-cols-3">
                      {plainCodes.codes.map(c => (
                        <div key={c} className="rounded-lg border bg-background px-3 py-1.5 font-mono text-sm">{c}</div>
                      ))}
                    </div>
                    <div className="flex gap-2">
                      <Button size="sm" variant="outline" onClick={async () => { await navigator.clipboard.writeText(plainCodes.codes.join("\n")); toast.success("已复制"); }}>
                        <Copy className="size-3.5" /> 复制
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => dlCodes(plainCodes)}>
                        <Download className="size-3.5" /> 下载
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => { setPlainCodes(null); saveBundle(null); }}>
                        <EyeOff className="size-3.5" /> 隐藏
                      </Button>
                    </div>
                  </div>
                ) : null}

                {/* 生成/重置 */}
                <Separator />
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field label="验证码"><Input className="h-8 text-sm" placeholder="当前验证码" value={recoveryCode} onChange={e => setRecoveryCode(e.target.value)} /></Field>
                  <Field label="或恢复码"><Input className="h-8 text-sm" placeholder="恢复码" value={recoveryFallback} onChange={e => setRecoveryFallback(e.target.value)} /></Field>
                </div>
                <div className="flex gap-2">
                  <Button size="sm" variant="outline" onClick={() => void handleGenCodes(false)} disabled={genCodes.isPending || !status.twoFactorEnabled}>
                    {genCodes.isPending ? "生成中..." : "生成恢复码"}
                  </Button>
                  <Button size="sm" onClick={() => void handleGenCodes(true)} disabled={regenCodes.isPending || !status.twoFactorEnabled}>
                    <RefreshCw className="size-3.5" /> {regenCodes.isPending ? "重置中..." : "重新生成"}
                  </Button>
                </div>
              </>
            )}
          </AccordionContent>
        </AccordionItem>

        {/* ━━ Passkey ━━ */}
        <AccordionItem value="passkey" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">
            <span className="flex items-center gap-2">
              <Fingerprint className="size-4 text-muted-foreground" />
              Passkey / WebAuthn
              <Badge variant={passkeyItems.length > 0 ? "success" : "outline"} className="ml-2">
                {passkeyItems.length > 0 ? `${passkeyItems.length} 个` : "未绑定"}
              </Badge>
            </span>
          </AccordionTrigger>
          <AccordionContent className="pb-4 space-y-4">
            {!passkeyOk ? (
              <p className="text-sm text-muted-foreground">Passkey 模块未在平台安全中启用。</p>
            ) : (
              <>
                {/* 注册新 Passkey */}
                <div className="flex items-center gap-3">
                  <Input className="h-8 text-sm flex-1" placeholder="设备名称（如：工作电脑）" value={passkeyName} onChange={e => setPasskeyName(e.target.value)} />
                  <Button size="sm" onClick={() => void handleRegisterPasskey()} disabled={beginPasskey.isPending || finishPasskey.isPending || !passkeySupport}>
                    {beginPasskey.isPending || finishPasskey.isPending ? "处理中..." : "注册 Passkey"}
                  </Button>
                </div>
                {!passkeySupport && <p className="text-xs text-amber-500">当前浏览器不支持 Passkey</p>}

                {/* 已绑定列表 */}
                {passkeyItems.length > 0 && (
                  <div className="space-y-2">
                    {passkeyItems.map(item => (
                      <div key={item.credentialId} className="flex items-center justify-between rounded-xl border px-4 py-3">
                        <div className="min-w-0">
                          <div className="text-sm font-medium truncate">{item.credentialName || "未命名设备"}</div>
                          <div className="flex gap-3 text-xs text-muted-foreground">
                            <span className="font-mono">{item.credentialId.slice(0, 12)}...</span>
                            <span>使用 {item.signCount} 次</span>
                            <span>{fmtDate(item.lastUsedAt || item.createdAt)}</span>
                          </div>
                        </div>
                        <Button size="sm" variant="ghost" className="text-destructive hover:text-destructive shrink-0" onClick={() => void handleDeletePasskey(item.credentialId)} disabled={deletePasskey.isPending}>
                          <Trash2 className="size-3.5" />
                        </Button>
                      </div>
                    ))}
                  </div>
                )}
              </>
            )}
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  );
}

// ── 辅助组件 ──

function StatusCard({ icon: Icon, label, value, ok, sub }: {
  icon: React.ComponentType<{ className?: string }>; label: string; value: string; ok?: boolean; sub?: string;
}) {
  return (
    <div className="flex items-center gap-3 rounded-xl border px-4 py-3">
      <Icon className={`size-5 shrink-0 ${ok ? "text-emerald-500" : "text-muted-foreground"}`} />
      <div className="min-w-0">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className="text-sm font-semibold truncate">{value}</div>
        {sub && <div className="text-[10px] text-muted-foreground truncate">{sub}</div>}
      </div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="space-y-1.5"><Label className="text-xs">{label}</Label>{children}</div>;
}
