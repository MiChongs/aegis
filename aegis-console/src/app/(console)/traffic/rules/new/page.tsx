"use client";

import { Suspense, useMemo } from "react";
import { useSearchParams } from "next/navigation";
import { LoadingState } from "@/components/ui/data-state";
import { RuleEditor } from "@/components/traffic/rule-editor";
import { TrafficGate } from "@/components/traffic/traffic-gate";
import { blankRule, normalizeRuleShape } from "@/components/traffic/traffic-format";
import { useTrafficConfigQuery, useTrafficTemplatesQuery } from "@/lib/traffic-hooks";

/** 新建规则；`?template=<id>` 从模板起步 */
function NewRuleInner() {
  const searchParams = useSearchParams();
  const templateId = searchParams.get("template");
  const templatesQuery = useTrafficTemplatesQuery(Boolean(templateId));
  const configQuery = useTrafficConfigQuery();
  const template = templateId ? templatesQuery.data?.find((item) => item.id === templateId) : undefined;

  const initial = useMemo(() => (template ? { ...normalizeRuleShape(template.rule), id: "" } : blankRule()), [template]);

  if (templateId && templatesQuery.isLoading) return <LoadingState title="加载中" />;

  return <RuleEditor key={template?.id ?? "blank"} initial={initial} defaultResponse={configQuery.data?.settings.response} />;
}

export default function NewTrafficRulePage() {
  return (
    <TrafficGate title="新建规则">
      <Suspense>
        <NewRuleInner />
      </Suspense>
    </TrafficGate>
  );
}
