"use client";

import { useState } from "react";
import { CodeBlock } from "@/components/developers/code-block";
import { CodeLanguageIcon, codeLanguage } from "@/lib/code-languages";
import type { Scenario } from "@/lib/integration-snippets";
import { cn } from "@/lib/utils";

/**
 * 场景 × 语言的示例浏览器。
 *
 * 两级选择用不同的视觉重量：场景是分段控件，一次只看一个流程；
 * 语言是带品牌图标的胶囊，便于快速定位。
 *
 * 切换场景时保留已选语言。用 Python 的人切到「短信登录」应当仍看到 Python，
 * 而不是被重置回第一个 Tab。
 */
export function CodeSamples({
  scenarios,
  maxHeight = 460,
  className
}: {
  scenarios: Scenario[];
  maxHeight?: number;
  className?: string;
}) {
  const [scenarioId, setScenarioId] = useState(scenarios[0]?.id ?? "");
  const [preferredLang, setPreferredLang] = useState<string | null>(null);

  const scenario = scenarios.find((item) => item.id === scenarioId) ?? scenarios[0];
  const snippets = scenario?.snippets ?? [];
  // 记住的语言在当前场景没有对应示例时，回落到该场景的第一项。
  // 数组最多十来项，直接 find 比 memo 的心智负担更小。
  const active = snippets.find((item) => item.lang === preferredLang) ?? snippets[0];

  if (!scenario || !active) return null;

  return (
    <div className={cn("space-y-3", className)}>
      {scenarios.length > 1 ? (
        <div className="flex flex-wrap gap-1 rounded-lg bg-muted/60 p-1">
          {scenarios.map((item) => (
            <button
              key={item.id}
              type="button"
              onClick={() => setScenarioId(item.id)}
              className={cn(
                "rounded-md px-3 py-1.5 text-sm transition-colors",
                item.id === scenario.id
                  ? "bg-background font-medium shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              )}
            >
              {item.label}
            </button>
          ))}
        </div>
      ) : null}

      <p className="text-[13px] leading-relaxed text-muted-foreground">{scenario.summary}</p>

      <div className="flex flex-wrap gap-1.5">
        {snippets.map((snippet) => {
          const meta = codeLanguage(snippet.lang);
          const selected = snippet.lang === active.lang;
          return (
            <button
              key={snippet.lang}
              type="button"
              onClick={() => setPreferredLang(snippet.lang)}
              className={cn(
                "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs transition-colors",
                selected
                  ? "border-primary/40 bg-primary/10 font-medium text-foreground"
                  : "text-muted-foreground hover:bg-muted"
              )}
              aria-pressed={selected}
            >
              <CodeLanguageIcon id={snippet.lang} className="size-3.5" />
              {meta.label}
            </button>
          );
        })}
      </div>

      <CodeBlock code={active.code} language={active.lang} maxHeight={maxHeight} />
    </div>
  );
}
