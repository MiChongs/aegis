"use client";

import { metrics } from "@/components/brand/home/home-content";
import { Reveal, SECTION_CONTAINER } from "@/components/brand/home/section";
import { CountUp, Grain, Pattern } from "@/components/brand/home/visuals";

/**
 * 数字带。
 *
 * 四个数字都是仓库里数得出来的事实（路由清单、渠道目录），每个下面跟一句
 * 它是怎么来的。没有出处的数字读者只能选择相信或者不信，两种反应对这一页
 * 都没有帮助。
 *
 * 数字进入视口时从 0 滚上来。SSR 输出的是最终值，滚动只是覆盖 textContent，
 * 因此没有 JS 或 reduce 档下页面上仍然是真实数字。
 */
export function MetricsSection() {
  return (
    <section className="relative overflow-hidden border-b bg-muted/30">
      <Pattern
        variant="dots"
        mask="linear-gradient(90deg, transparent, #000 20%, #000 80%, transparent)"
      />
      <Grain />

      <div className={`${SECTION_CONTAINER} relative`}>
        {/* 刻意不用 divide-x：多列网格上它按 DOM 顺序给「除第一个以外」全部
            加边框，第二行的首列会凭空多出一条竖线。间距足够分开四组数字。 */}
        <dl className="grid grid-cols-1 gap-x-8 gap-y-10 py-12 sm:grid-cols-2 lg:grid-cols-4 lg:py-14">
          {metrics.map((metric, index) => (
            <Reveal key={metric.label} delay={index * 0.06}>
              <dt className="sr-only">{metric.label}</dt>
              <dd>
                <span
                  className="block text-4xl font-semibold tracking-tight tabular-nums md:text-5xl"
                  style={{ fontFamily: "var(--font-data)" }}
                >
                  <CountUp value={metric.value} />
                </span>
                <span
                  aria-hidden
                  className="mt-3 block h-px w-10"
                  style={{
                    background:
                      "linear-gradient(90deg, var(--home-accent), transparent)",
                  }}
                />
                <span className="mt-3 block text-sm font-medium">{metric.label}</span>
                <span className="mt-1 block text-xs leading-relaxed text-muted-foreground">
                  {metric.hint}
                </span>
              </dd>
            </Reveal>
          ))}
        </dl>
      </div>
    </section>
  );
}
