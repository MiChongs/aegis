"use client";

import { useState } from "react";
import { Loader2, Puzzle } from "lucide-react";
import { toast } from "sonner";
import { Section, errorMessage } from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import type { CosmeticCatalog, CosmeticSettings, WorkshopSettings } from "@/lib/api/cosmetics";
import { useCosmeticSettingsQuery, useSaveCosmeticSettingsMutation } from "@/lib/cosmetic-hooks";
import { cn } from "@/lib/utils";

/**
 * 自定义工坊设置（docs/cosmetics.md「自定义工坊 / 额度」）：开关、免费用户额度、会员额度、按会员档位的额度。
 *
 * 一个人的额度 = max(免费额度, 有效会员时的会员额度, 所在档位的档位额度)，上限 50，工坊关闭时为 0。
 * 档位额度留空即不单独设置（走会员额度）。保存时 PUT /settings 带上 workshop，开关与勋章位照服务端现值回传；
 * 目录里已经没有的档位编号原样保留，不在这里悄悄删掉。
 */

const MAX_QUOTA = 50;

type Form = {
  enabled: boolean;
  freeQuota: string;
  vipQuota: string;
  /** 档位编号 → 额度（空串即不单独设置） */
  tiers: Record<string, string>;
};

function seed(settings?: CosmeticSettings | null): Form {
  const workshop = settings?.workshop;
  const tiers: Record<string, string> = {};
  for (const [key, value] of Object.entries(workshop?.tierQuotas ?? {})) tiers[key] = String(value);
  return {
    enabled: workshop?.enabled ?? false,
    freeQuota: String(workshop?.freeQuota ?? 0),
    vipQuota: String(workshop?.vipQuota ?? 0),
    tiers
  };
}

function parseQuota(value: string): number | null {
  if (!/^\d+$/.test(value.trim())) return null;
  const n = Number(value.trim());
  return n >= 0 && n <= MAX_QUOTA ? n : null;
}

function toSettings(form: Form): { value?: WorkshopSettings; problem?: string } {
  const freeQuota = parseQuota(form.freeQuota);
  if (freeQuota === null) return { problem: `免费用户额度需要在 0–${MAX_QUOTA} 之间` };
  const vipQuota = parseQuota(form.vipQuota);
  if (vipQuota === null) return { problem: `会员额度需要在 0–${MAX_QUOTA} 之间` };
  const tierQuotas: Record<string, number> = {};
  for (const [key, raw] of Object.entries(form.tiers)) {
    if (!raw.trim()) continue;
    const quota = parseQuota(raw);
    if (quota === null) return { problem: `会员档位额度需要在 0–${MAX_QUOTA} 之间` };
    tierQuotas[key] = quota;
  }
  return { value: { enabled: form.enabled, freeQuota, vipQuota, tierQuotas } };
}

function signature(form: Form) {
  const tiers = Object.entries(form.tiers)
    .filter(([, value]) => value.trim() !== "")
    .map(([key, value]) => `${key}=${value.trim()}`)
    .sort()
    .join(",");
  return `${form.enabled}|${form.freeQuota.trim()}|${form.vipQuota.trim()}|${tiers}`;
}

export function WorkshopSettingsCard({
  className,
  appKey,
  catalog,
  canWrite
}: {
  className?: string;
  appKey: string;
  catalog?: CosmeticCatalog | null;
  canWrite: boolean;
}) {
  const settingsQuery = useCosmeticSettingsQuery(appKey);
  const save = useSaveCosmeticSettingsMutation(appKey);
  const settings = settingsQuery.data;
  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);
  const server = seed(settings);
  const form = draft?.scope === appKey ? draft.value : server;
  const dirty = Boolean(settings) && signature(form) !== signature(server);
  const problem = toSettings(form).problem;
  const patch = (next: Partial<Form>) => setDraft({ scope: appKey, value: { ...form, ...next } });
  const tiers = catalog?.tiers ?? [];

  const submit = async (next: Form) => {
    if (!settings) return;
    const { value, problem: invalid } = toSettings(next);
    if (!value) {
      toast.error(invalid);
      return;
    }
    try {
      await save.mutateAsync({ enabled: settings.enabled, badgeSlots: settings.badgeSlots, workshop: value });
      toast.success(value.enabled !== server.enabled ? (value.enabled ? "自定义工坊已开启" : "自定义工坊已关闭") : "设置已保存");
      setDraft(null);
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    }
  };

  return (
    <Section className={className} title="自定义工坊" icon={Puzzle} tone="violet">
      {settingsQuery.isLoading || !settings ? (
        <Skeleton className="h-[160px] w-full rounded-lg" />
      ) : (
        <div className="flex flex-col gap-5">
          <label className="flex items-center justify-between gap-4">
            <span className="flex flex-col gap-0.5">
              <span className="text-sm font-medium">对用户开放</span>
              <span className="text-xs text-muted-foreground">
                {form.enabled ? (settings.enabled ? "工坊可用" : "工坊已开启，装扮中心未开启") : "工坊未开启"}
              </span>
            </span>
            <span className="flex items-center gap-2">
              {save.isPending ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> : null}
              <Switch
                checked={form.enabled}
                disabled={!canWrite || save.isPending}
                onCheckedChange={(enabled) => void submit({ ...form, enabled })}
                aria-label="对用户开放自定义工坊"
              />
            </span>
          </label>

          <div className="grid gap-4 sm:grid-cols-2">
            <QuotaField
              label="免费用户额度"
              value={form.freeQuota}
              disabled={!canWrite}
              onChange={(freeQuota) => patch({ freeQuota })}
            />
            <QuotaField label="会员额度" value={form.vipQuota} disabled={!canWrite} onChange={(vipQuota) => patch({ vipQuota })} />
          </div>

          <div className="flex flex-col gap-2">
            <Label className="text-xs text-muted-foreground">按会员档位</Label>
            {tiers.length ? (
              <div className="grid gap-2 sm:grid-cols-2">
                {tiers.map((tier) => {
                  const key = String(tier.id);
                  return (
                    <div key={tier.id} className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2">
                      <span className="truncate text-sm">{tier.name}</span>
                      <QuotaInput
                        value={form.tiers[key] ?? ""}
                        placeholder="—"
                        disabled={!canWrite}
                        ariaLabel={`${tier.name}额度`}
                        onChange={(value) => patch({ tiers: { ...form.tiers, [key]: value } })}
                      />
                    </div>
                  );
                })}
              </div>
            ) : (
              <span className="text-xs text-muted-foreground">暂无会员档位</span>
            )}
          </div>

          <div className="flex items-center justify-between gap-2 border-t pt-4">
            <span className={cn("text-xs", dirty && problem ? "text-destructive" : "text-muted-foreground")}>
              {dirty && problem ? problem : `上限 ${MAX_QUOTA} 件`}
            </span>
            <Button size="sm" disabled={!canWrite || !dirty || save.isPending} onClick={() => void submit(form)}>
              {save.isPending ? <Loader2 className="animate-spin" /> : null}
              保存
            </Button>
          </div>
        </div>
      )}
    </Section>
  );
}

function QuotaField({
  label,
  value,
  disabled,
  onChange
}: {
  label: string;
  value: string;
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      <QuotaInput value={value} disabled={disabled} ariaLabel={label} onChange={onChange} />
    </div>
  );
}

function QuotaInput({
  value,
  placeholder,
  disabled,
  ariaLabel,
  onChange
}: {
  value: string;
  placeholder?: string;
  disabled?: boolean;
  ariaLabel: string;
  onChange: (value: string) => void;
}) {
  return (
    <span className="flex items-center gap-1.5">
      <Input
        value={value}
        inputMode="numeric"
        maxLength={2}
        placeholder={placeholder}
        disabled={disabled}
        aria-label={ariaLabel}
        className="h-8 w-16 text-center tabular-nums"
        onChange={(event) => onChange(event.target.value.replace(/\D/g, ""))}
      />
      <span className="text-xs text-muted-foreground">件</span>
    </span>
  );
}
