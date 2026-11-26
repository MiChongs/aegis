"use client";

import { useState } from "react";
import { Skeleton } from "@/components/ui/skeleton";
import { TabStack } from "@/components/traffic/traffic-kit";
import { useFirewallCatalogQuery } from "@/lib/firewall-hooks";
import { cn } from "@/lib/utils";
import { ReadOnlyNotice, SaveBar, SettingsMeta, useSettingsDraft, type SettingsKey } from "./settings-draft";
import { DirectivesButton } from "./waf-directives";
import { EnginePanel, InspectionPanels, MasterSwitchPanel, SensitivityPanel, engineModeOf, wafValidation } from "./waf-engine";
import { RemovedRulesPanel, RuleGroupsPanel } from "./waf-groups";
import { RuleDetailDialog, RuleLibraryPanel } from "./waf-rule-library";
import type { FirewallTab } from "./waf-shared";
import { TuningPanel, tuningValidation } from "./waf-tuning";

/**
 * 防火墙 → WAF 引擎（载荷段，docs/firewall.md §2-§4）。
 *
 * 整页编辑一份设置草稿，吸底条一次保存（部分更新，只发改过的字段）。后端保存前会用改动后的
 * 配置试编译 WAF，编不过返回 40090 并带原因，草稿保留不丢。规则组卡片、规则库的状态都按草稿算，
 * 保存之前就能看到「停了这组 / 提高检测等级」之后哪些规则会生效。
 */

const WAF_KEYS = [
  "enabled",
  "corazaEnabled",
  "corazaDetectionOnly",
  "corazaParanoia",
  "corazaDetectionParanoia",
  "corazaAnomalyThreshold",
  "corazaOutboundThreshold",
  "corazaResponseInspection",
  "corazaResponseBodyLimit",
  "corazaInspectAdminBody",
  "requestBodyLimit",
  "requestBodyMemory",
  "corazaArgumentsLimit",
  "corazaSensitiveArgs",
  "corazaRichTextArgs",
  "corazaBodyExemptPrefixes",
  "corazaExtraContentTypes",
  "corazaDisabledGroups",
  "corazaRemovedRules"
] as const satisfies readonly SettingsKey[];

export function FirewallWafTab({ onNavigate }: { onNavigate: (tab: FirewallTab) => void }) {
  const draft = useSettingsDraft(WAF_KEYS);
  const catalogQuery = useFirewallCatalogQuery();
  const catalog = catalogQuery.data;
  const [openRule, setOpenRule] = useState<number | null>(null);
  const { form, settings, set, update, canWrite } = draft;

  if (!form || !settings) {
    if (draft.query.isError) {
      return <div className="rounded-2xl border border-border bg-card py-16 text-center text-sm text-muted-foreground">设置加载失败</div>;
    }
    return (
      <TabStack>
        <Skeleton className="h-12 rounded-xl" />
        <div className="grid gap-4 @4xl:grid-cols-[minmax(0,1fr)_minmax(0,320px)]">
          <Skeleton className="h-40 rounded-2xl" />
          <Skeleton className="h-40 rounded-2xl" />
        </div>
        <Skeleton className="h-96 rounded-2xl" />
      </TabStack>
    );
  }

  const wafOff = engineModeOf(form) === "off" || !form.enabled;
  const blockedReason = wafValidation(form) ?? tuningValidation(form);

  const toggleRemoved = (id: number, removed: boolean) => {
    const text = String(id);
    const rest = form.corazaRemovedRules.filter((item) => item !== text);
    set("corazaRemovedRules", removed ? [...rest, text].sort((a, b) => Number(a) - Number(b)) : rest);
  };

  return (
    <TabStack className={cn(draft.dirty && "pb-4")}>
      <SettingsMeta settings={settings} aside={<DirectivesButton />} />
      {!canWrite ? <ReadOnlyNotice /> : null}

      <fieldset disabled={!canWrite || draft.pending} className="m-0 flex min-w-0 flex-col gap-4 border-0 p-0">
        <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1fr)_minmax(0,320px)]">
          <EnginePanel form={form} update={update} crsVersion={catalog?.version} />
          <MasterSwitchPanel form={form} set={set} />
        </div>
        <SensitivityPanel form={form} update={update} dimmed={wafOff} />
        <InspectionPanels form={form} set={set} dimmed={wafOff} />
        <TuningPanel form={form} set={set} dimmed={wafOff} />
        <RuleGroupsPanel form={form} update={update} catalog={catalog} dimmed={wafOff} />
        <RemovedRulesPanel form={form} set={set} catalog={catalog} onNavigate={onNavigate} onOpenRule={setOpenRule} dimmed={wafOff} />
      </fieldset>

      <RuleLibraryPanel form={form} catalog={catalog} dirty={draft.dirty} onOpenRule={setOpenRule} />

      <RuleDetailDialog
        ruleId={openRule}
        onClose={() => setOpenRule(null)}
        form={form}
        catalog={catalog}
        canWrite={canWrite}
        onToggleRemoved={toggleRemoved}
        onNavigate={onNavigate}
      />

      {canWrite ? (
        <SaveBar count={draft.changed.length} pending={draft.pending} blockedReason={blockedReason} onDiscard={draft.discard} onSave={draft.submit} />
      ) : null}
    </TabStack>
  );
}
