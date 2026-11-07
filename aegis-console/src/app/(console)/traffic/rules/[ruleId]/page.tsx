"use client";

import { useMemo } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { RuleEditor } from "@/components/traffic/rule-editor";
import { TrafficGate } from "@/components/traffic/traffic-gate";
import { normalizeRuleShape } from "@/components/traffic/traffic-format";
import { trafficErrorMessage, useTrafficConfigQuery } from "@/lib/traffic-hooks";

function EditRuleInner({ ruleId }: { ruleId: string }) {
  const configQuery = useTrafficConfigQuery();
  const source = configQuery.data?.rules.find((rule) => rule.id === ruleId);
  // 草稿优先于底稿：配置刷新只在还没改过时换掉底稿，已有改动不受影响
  const initial = useMemo(() => (source ? normalizeRuleShape(source) : null), [source]);

  if (configQuery.isLoading) return <LoadingState title="加载中" />;

  if (!initial) {
    return (
      <div className="page-stack">
        <SectionHeading
          eyebrow="流量管理"
          title="编辑规则"
          action={
            <Button size="sm" variant="ghost" asChild>
              <Link href="/traffic?tab=rules">
                <ArrowLeft />
                返回
              </Link>
            </Button>
          }
        />
        <EmptyState title={configQuery.isError ? trafficErrorMessage(configQuery.error, "读取失败") : "规则不存在"} />
      </div>
    );
  }

  return <RuleEditor key={initial.id} initial={initial} ruleId={initial.id} defaultResponse={configQuery.data?.settings.response} />;
}

export default function EditTrafficRulePage() {
  const params = useParams<{ ruleId: string }>();
  const ruleId = decodeURIComponent(params.ruleId ?? "");
  return (
    <TrafficGate title="编辑规则">
      <EditRuleInner ruleId={ruleId} />
    </TrafficGate>
  );
}
