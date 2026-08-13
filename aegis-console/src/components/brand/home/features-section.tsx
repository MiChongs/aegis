"use client";

import { Check } from "lucide-react";
import {
  FunctionVisual,
  LedgerVisual,
  PolicyVisual,
  ProtocolVisual,
  RiskVisual,
  TenancyVisual,
} from "@/components/brand/home/feature-visuals";
import { features, type HomeFeatureVisual } from "@/components/brand/home/home-content";
import { Reveal, Section, SectionHeading } from "@/components/brand/home/section";
import { Grain, Pattern, SpotlightCard } from "@/components/brand/home/visuals";
import { cn } from "@/lib/utils";

const VISUALS: Record<HomeFeatureVisual, () => React.ReactElement> = {
  tenancy: TenancyVisual,
  policy: PolicyVisual,
  protocol: ProtocolVisual,
  risk: RiskVisual,
  ledger: LedgerVisual,
  function: FunctionVisual,
};

/**
 * 核心能力（bento）。
 *
 * 每张卡的标题是一句**结论**而不是能力名：「多租户隔离」只说明了这里有个
 * 名词，「应用之间的用户数据天然隔离」才说清它对读者意味着什么。
 * 名词降级成卡片左上角的 eyebrow，需要检索的人仍然找得到。
 *
 * 每张卡另外配一张示意图。六张只有文字的卡片摆在一屏里会退化成一段被切碎的
 * 说明书，读者的眼睛没有落点；图形不承担信息，但它给了每张卡一个可区分的形状。
 */
export function FeaturesSection() {
  return (
    <Section id="features" className="relative overflow-hidden">
      <Pattern
        variant="dots"
        mask="radial-gradient(ellipse 60% 50% at 50% 40%, #000, transparent 78%)"
      />
      <Grain />

      <div className="relative">
        <SectionHeading
          eyebrow="核心能力"
          title="六项已在平台层完成的基础能力"
          description="以下能力在多数项目中需要各自实现一遍。Aegis 将其收敛至平台层统一提供，并由测试覆盖。"
        />

        <div className="mt-12 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {features.map((feature, index) => {
            const Visual = VISUALS[feature.visual];
            return (
              <Reveal
                key={feature.title}
                delay={(index % 3) * 0.06}
                className={cn(feature.wide && "lg:col-span-2")}
              >
                <SpotlightCard
                  as="article"
                  className="flex h-full flex-col gap-5 rounded-xl border bg-card p-5 text-card-foreground shadow-sm transition-colors duration-300 hover:border-[color-mix(in_srgb,var(--home-accent)_40%,var(--border))] md:p-6"
                >
                  <div className="relative flex items-center gap-2.5">
                    <span
                      className="flex size-8 items-center justify-center rounded-md border"
                      style={{
                        background: "color-mix(in srgb, var(--home-accent) 10%, transparent)",
                        borderColor: "color-mix(in srgb, var(--home-accent) 26%, transparent)",
                      }}
                    >
                      <feature.icon
                        className="size-4"
                        strokeWidth={1.75}
                        style={{ color: "var(--home-accent)" }}
                      />
                    </span>
                    <span className="text-xs font-medium tracking-[0.14em] text-muted-foreground uppercase">
                      {feature.eyebrow}
                    </span>
                  </div>

                  <div className="relative">
                    <h3 className="text-lg leading-snug font-semibold text-balance">
                      {feature.title}
                    </h3>
                    <p className="mt-2.5 text-sm leading-relaxed text-pretty text-muted-foreground">
                      {feature.body}
                    </p>
                  </div>

                  <div className="relative">
                    <Visual />
                  </div>

                  <ul className="relative mt-auto flex flex-col gap-2 border-t pt-4">
                    {feature.points.map((point) => (
                      <li key={point} className="flex items-start gap-2 text-sm">
                        <Check
                          className="mt-0.5 size-3.5 shrink-0"
                          strokeWidth={2.5}
                          style={{ color: "var(--home-accent)" }}
                        />
                        <span className="text-muted-foreground">{point}</span>
                      </li>
                    ))}
                  </ul>
                </SpotlightCard>
              </Reveal>
            );
          })}
        </div>
      </div>
    </Section>
  );
}
