"use client";

import { archLayers } from "@/components/brand/home/home-content";
import { Reveal, Section, SectionHeading } from "@/components/brand/home/section";
import { Grain, Pattern, SpotlightCard } from "@/components/brand/home/visuals";

/**
 * 架构分层。
 *
 * 每层写清「它负责什么」而不只是「它由什么组成」：后者是一串技术名词，
 * 回答不了读者真正在问的那个问题，也就是一次请求会经过哪些地方、
 * 判定发生在哪一层。
 *
 * 竖线上有一颗沿着五层往下走的光点。它不承担信息，作用是把这张静态清单
 * 读成"一次请求的路径"，这正是这一节想说的事。reduce 档下由 CSS 停掉。
 */
export function ArchitectureSection() {
  return (
    <Section id="architecture" bordered className="relative overflow-hidden">
      <Pattern
        size={72}
        mask="radial-gradient(ellipse 70% 60% at 50% 50%, #000, transparent 80%)"
      />
      <Grain />

      <div className="relative grid gap-10 lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)] lg:gap-16">
        <div className="lg:sticky lg:top-24 lg:self-start">
          <SectionHeading
            eyebrow="架构"
            title="请求经过的五层结构"
            description="自上而下每层职责单一。权限与作用域校验统一下沉至业务层，因此控制台、SDK 与远程函数等不同入口均无法绕过判定。"
          />
        </div>

        <ol className="relative flex flex-col">
          {/* 轨道：一条渐隐的竖线 + 一颗循环下行的光点 */}
          <span
            aria-hidden
            className="absolute top-2 bottom-2 left-[15px] w-px md:left-[19px]"
            style={{
              background:
                "linear-gradient(180deg, transparent, var(--border) 8%, var(--border) 92%, transparent)",
            }}
          />
          <span
            aria-hidden
            className="home-request-dot absolute left-[13px] size-[5px] rounded-full md:left-[17px]"
            style={{
              background: "var(--home-accent)",
              boxShadow: "0 0 12px 2px var(--home-beam)",
            }}
          />

          {/* Reveal 在 li 之内而不是之外：ol 的合法子元素只有 li，
              中间夹一层 div 会让这份清单不再是清单（读屏软件也不再报条目数）。 */}
          {archLayers.map((layer, index) => (
            <li key={layer.name} className="relative flex gap-4 pb-6 last:pb-0 md:gap-5">
              <span
                className="relative z-10 flex size-8 shrink-0 items-center justify-center rounded-full border bg-background font-mono text-xs md:size-10"
                style={{
                  borderColor: "color-mix(in srgb, var(--home-accent) 28%, var(--border))",
                  color: "var(--home-accent)",
                }}
                aria-hidden
              >
                {String(index + 1).padStart(2, "0")}
              </span>
              <Reveal delay={index * 0.05} className="min-w-0 flex-1">
                <SpotlightCard className="rounded-lg border bg-card p-4 transition-colors duration-300 hover:border-[color-mix(in_srgb,var(--home-accent)_40%,var(--border))] md:p-5">
                  <h3 className="relative text-sm font-semibold">{layer.name}</h3>
                  <p className="relative mt-1.5 text-sm leading-relaxed text-muted-foreground">
                    {layer.role}
                  </p>
                  <p className="relative mt-3 border-t pt-3 font-mono text-xs break-words text-muted-foreground/80">
                    {layer.stack}
                  </p>
                </SpotlightCard>
              </Reveal>
            </li>
          ))}
        </ol>
      </div>
    </Section>
  );
}
