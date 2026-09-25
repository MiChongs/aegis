"use client";

import { useState } from "react";
import {
  ChevronDown,
  CircleAlert,
  Eye,
  EyeOff,
  KeyRound,
  Lock,
  RefreshCw,
  Save,
  Shield,
  Wand2
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { RadioGroup } from "@/components/ui/radio-group";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import { useAdminAppEncryptionQuery, useUpdateAdminAppEncryptionMutation } from "@/lib/admin-hooks";
import {
  FieldGroup,
  ModeCard,
  SectionCard,
  StatusDot,
  SwitchRow
} from "@/components/apps/app-config-primitives";

/**
 * 旧 `/api/auth/*` + `/api/user/*` 命名空间的传输加密。
 *
 * **不要和「接入安全等级」搞混** —— 平台有两套互不相干的加密机制：
 *
 * | 机制 | 作用域 | 在哪配 |
 * |---|---|---|
 * | 本面板（transportEncryption） | 旧明文命名空间 `/api/auth`、`/api/user`，由 `allowLegacy` 开关控制是否可达 | 这里 |
 * | 安全等级 sealed + Transport v2 密钥 | 新接入网关 `/api/v1/apps/{appKey}/*` | 上方「接入配置」 |
 *
 * 它原先被放在「策略配置」Tab 里，与登录策略混编 —— 那里既看不出它只管旧命名空间，
 * 也看不出它和安全等级的关系。挪到「接入」是因为所有线路协议相关配置应当同处一屏。
 */
type EncryptionForm = {
  enabled: boolean;
  mode: "strict" | "lenient";
  responseEncryption: boolean;
  selectedAlgos: string[];
};

export function AppTransportEncryptionPanel({ appKey }: { appKey?: string | null }) {
  const query = useAdminAppEncryptionQuery(appKey);
  const mutation = useUpdateAdminAppEncryptionMutation(appKey);
  const data = query.data;

  // 密钥输入框独立于草稿：它永远从空开始（编辑态不回显已存密钥），
  // 不该跟着服务端数据被重置逻辑影响
  const [secret, setSecret] = useState("");
  const [showSecret, setShowSecret] = useState(false);

  // 其余字段按 appKey 绑定作用域，无草稿时从服务端数据派生（不用 useEffect 同步）
  const [draft, setDraft] = useState<{ scope: string; value: EncryptionForm } | null>(null);
  const scope = appKey ?? "";
  const form: EncryptionForm =
    draft?.scope === scope
      ? draft.value
      : {
          enabled: data?.enabled ?? false,
          mode: data?.strict === false ? "lenient" : "strict",
          responseEncryption: data?.responseEncryption ?? true,
          selectedAlgos: data?.allowedAlgorithms ?? []
        };
  const { enabled, mode, responseEncryption, selectedAlgos } = form;
  const patch = <K extends keyof EncryptionForm>(key: K, value: EncryptionForm[K]) =>
    setDraft({ scope, value: { ...form, [key]: value } });

  async function handleSave() {
    if (enabled && selectedAlgos.length === 0) {
      toast.error("启用传输加密时至少要选一种算法");
      return;
    }
    const payload: Record<string, unknown> = {
      enabled,
      strict: mode === "strict",
      responseEncryption,
      allowedAlgorithms: selectedAlgos
    };
    if (secret.trim()) payload.secret = secret.trim();
    try {
      await mutation.mutateAsync(payload as never);
      toast.success("传输加密配置已保存");
      setSecret("");
      setShowSecret(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  async function handleGenerateKeys(type: "rsa" | "ecdh") {
    try {
      await mutation.mutateAsync({ [type === "rsa" ? "generateRSAKey" : "generateECDHKey"]: true } as never);
      toast.success(`${type.toUpperCase()} 密钥对已生成`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "生成失败");
    }
  }

  function generateSecret() {
    const bytes = new Uint8Array(32);
    crypto.getRandomValues(bytes);
    setSecret(Array.from(bytes).map((b) => b.toString(16).padStart(2, "0")).join(""));
    setShowSecret(true);
  }

  if (!appKey) return null;
  if (query.isLoading) {
    return (
      <SectionCard icon={<Lock className="size-4" />} title="传输加密（旧命名空间）">
        <Skeleton className="h-32 w-full" />
      </SectionCard>
    );
  }

  return (
    <SectionCard
      icon={<Lock className="size-4" />}
      title="传输加密（旧命名空间）"
      aside={<StatusDot active={enabled} labelActive="已启用" labelInactive="未启用" />}
    >
      <div className="space-y-6">
        <FieldGroup label="基础设置">
          <div className="grid gap-2.5 sm:grid-cols-2">
            <SwitchRow
              label="启用传输加密"
              checked={enabled}
              onChange={(v) => patch("enabled", v)}
            />
            <SwitchRow
              label="加密响应数据"
              checked={responseEncryption}
              onChange={(v) => patch("responseEncryption", v)}
              disabled={!enabled}
            />
          </div>
        </FieldGroup>

        <FieldGroup label="兼容模式">
          <RadioGroup value={mode} onValueChange={(v) => patch("mode", v as "strict" | "lenient")} disabled={!enabled}>
            <div className="grid gap-2 sm:grid-cols-2">
              <ModeCard
                value="strict"
                active={mode === "strict"}
                title="严格模式"
                description="拒绝任何明文请求"
                icon={<Shield className="size-4" />}
                disabled={!enabled}
              />
              <ModeCard
                value="lenient"
                active={mode === "lenient"}
                title="宽松模式"
                description="兼容明文请求"
                icon={<CircleAlert className="size-4" />}
                disabled={!enabled}
              />
            </div>
          </RadioGroup>
        </FieldGroup>

        <FieldGroup label="允许的加密算法" hint="至少启用一种算法">
          <div className="space-y-4">
            <AlgoGroup
              title="对称加密"
              disabled={!enabled}
              algos={SYMMETRIC_ALGOS}
              selected={selectedAlgos}
              onToggle={(next) => patch("selectedAlgos", next)}
            />
            <AlgoGroup
              title="混合加密"
              disabled={!enabled}
              algos={HYBRID_ALGOS}
              selected={selectedAlgos}
              onToggle={(next) => patch("selectedAlgos", next)}
            />
          </div>
        </FieldGroup>

        <FieldGroup label="对称密钥" hint={data?.secretHint ? `当前指纹：${data.secretHint}` : undefined}>
          <div className="flex flex-col gap-2 sm:flex-row">
            <div className="relative flex-1">
              <Input
                type={showSecret ? "text" : "password"}
                className="pr-9 font-mono text-xs"
                placeholder={data?.hasSecret ? "留空不修改" : "输入密钥或点击生成"}
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
              />
              {secret && (
                <button
                  type="button"
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  onClick={() => setShowSecret(!showSecret)}
                  aria-label={showSecret ? "隐藏密钥" : "显示密钥"}
                >
                  {showSecret ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </button>
              )}
            </div>
            <Button type="button" size="sm" variant="outline" onClick={generateSecret}>
              <Wand2 className="size-3.5" />
              生成随机密钥
            </Button>
          </div>
        </FieldGroup>

        <FieldGroup label="非对称密钥对">
          <div className="grid gap-3 sm:grid-cols-2">
            <KeyPairCard
              title="RSA 密钥对"
              subtitle="RSA-2048"
              hasKey={!!data?.hasRSAKey}
              publicKey={data?.rsaPublicKey}
              pending={mutation.isPending}
              onGenerate={() => handleGenerateKeys("rsa")}
            />
            <KeyPairCard
              title="ECDH 密钥对"
              subtitle="ECDH P-256"
              hasKey={!!data?.hasECDHKey}
              publicKey={data?.ecdhPublicKey}
              pending={mutation.isPending}
              onGenerate={() => handleGenerateKeys("ecdh")}
            />
          </div>
        </FieldGroup>

        <div className="flex justify-end">
          <Button size="sm" disabled={mutation.isPending} onClick={handleSave}>
            <Save className="size-3.5" />
            {mutation.isPending ? "保存中..." : "保存加密配置"}
          </Button>
        </div>
      </div>
    </SectionCard>
  );
}

function AlgoGroup({
  title,
  algos,
  selected,
  disabled,
  onToggle
}: {
  title: string;
  algos: AlgoItem[];
  selected: string[];
  disabled?: boolean;
  onToggle: (next: string[]) => void;
}) {
  return (
    <div className="space-y-2">
      <div className="flex items-baseline justify-between">
        <div className="text-xs font-medium">{title}</div>
      </div>
      <ToggleGroup
        type="multiple"
        value={selected}
        onValueChange={onToggle}
        variant="outline"
        size="sm"
        className="gap-2"
        disabled={disabled}
      >
        {algos.map((a) => (
          <HoverCard key={a.value} openDelay={250}>
            <HoverCardTrigger asChild>
              <ToggleGroupItem
                value={a.value}
                className="h-auto rounded-lg border-0 bg-muted px-3 py-2 text-xs transition-colors duration-150 hover:bg-muted-foreground/10 data-[state=on]:bg-emerald-500/10 data-[state=on]:text-emerald-600 data-[state=on]:ring-1 data-[state=on]:ring-inset data-[state=on]:ring-emerald-500/30 dark:data-[state=on]:text-emerald-400"
              >
                <span className="font-mono">{a.label}</span>
              </ToggleGroupItem>
            </HoverCardTrigger>
            <HoverCardContent className="w-64 text-xs">
              <div className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-semibold">{a.label}</span>
                  <span className="inline-flex items-center rounded-md bg-muted px-1.5 py-0.5 text-[9px] font-medium uppercase tracking-wider text-muted-foreground">
                    {a.kind}
                  </span>
                </div>
                <p className="leading-relaxed text-muted-foreground">{a.description}</p>
                {a.recommended && (
                  <div className="flex items-center gap-1.5 pt-0.5 text-[10px] font-medium text-emerald-600 dark:text-emerald-400">
                    <span className="size-1.5 rounded-full bg-emerald-500" />
                    官方推荐
                  </div>
                )}
              </div>
            </HoverCardContent>
          </HoverCard>
        ))}
      </ToggleGroup>
    </div>
  );
}

function KeyPairCard({
  title,
  subtitle,
  hasKey,
  publicKey,
  pending,
  onGenerate
}: {
  title: string;
  subtitle: string;
  hasKey: boolean;
  publicKey?: string;
  pending: boolean;
  onGenerate: () => void;
}) {
  const [viewOpen, setViewOpen] = useState(false);

  return (
    <div className="space-y-3 rounded-xl bg-muted p-3.5">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 space-y-0.5">
          <div className="flex items-center gap-2 text-sm font-medium">
            <KeyRound className="size-3.5 text-muted-foreground" />
            {title}
          </div>
          <div className="font-mono text-[10px] text-muted-foreground">{subtitle}</div>
        </div>
        <StatusDot active={hasKey} labelActive="已生成" labelInactive="未生成" />
      </div>
      <div className="flex flex-wrap gap-2">
        {hasKey ? (
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button size="sm" variant="outline" disabled={pending} className="h-7 text-[11px]">
                <RefreshCw className="size-3" />重新生成
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>确认重新生成 {title}？</AlertDialogTitle>
                <AlertDialogDescription>
                  旧密钥对立即失效，在途数据将无法解密。
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>取消</AlertDialogCancel>
                <AlertDialogAction
                  className="bg-destructive text-white hover:bg-destructive/90"
                  onClick={onGenerate}
                >
                  确认重新生成
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        ) : (
          <Button size="sm" variant="outline" disabled={pending} onClick={onGenerate} className="h-7 text-[11px]">
            <Wand2 className="size-3" />生成密钥对
          </Button>
        )}
        {publicKey && (
          <Collapsible open={viewOpen} onOpenChange={setViewOpen}>
            <CollapsibleTrigger asChild>
              <Button size="sm" variant="ghost" className="h-7 text-[11px]">
                <ChevronDown className={cn("size-3 transition-transform", viewOpen && "rotate-180")} />
                {viewOpen ? "收起公钥" : "查看公钥"}
              </Button>
            </CollapsibleTrigger>
            <CollapsibleContent className="pt-2">
              <pre className="max-h-32 overflow-auto rounded-lg bg-card p-2 font-mono text-[9px] leading-relaxed">
                {publicKey}
              </pre>
            </CollapsibleContent>
          </Collapsible>
        )}
      </div>
    </div>
  );
}

type AlgoItem = { value: string; label: string; kind: string; description: string; recommended?: boolean };

const SYMMETRIC_ALGOS: AlgoItem[] = [
  {
    value: "XChaCha20Poly1305",
    label: "XChaCha20-Poly1305",
    kind: "对称",
    description: "AEAD 流密码，移动端首选",
    recommended: true
  },
  {
    value: "AES-256-GCM",
    label: "AES-256-GCM",
    kind: "对称",
    description: "硬件加速，适合服务端"
  }
];

const HYBRID_ALGOS: AlgoItem[] = [
  {
    value: "hybrid-rsa-xchacha20",
    label: "RSA + XChaCha20",
    kind: "混合",
    description: "兼容性最好"
  },
  {
    value: "hybrid-rsa-aes256gcm",
    label: "RSA + AES-256-GCM",
    kind: "混合",
    description: "RSA-2048 信封"
  },
  {
    value: "hybrid-ecdh-xchacha20",
    label: "ECDH + XChaCha20",
    kind: "混合",
    description: "密钥尺寸更小",
    recommended: true
  },
  {
    value: "hybrid-ecdh-aes256gcm",
    label: "ECDH + AES-256-GCM",
    kind: "混合",
    description: "ECDH P-256 协商"
  }
];
