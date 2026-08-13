"use client";

import { useReducedMotion } from "motion/react";
import {
  Marquee,
  MarqueeContent,
  MarqueeFade,
  MarqueeItem,
} from "@/components/kibo-ui/marquee";
import { Item } from "@/components/ui/item";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { BrandLogo } from "@/components/brand/sponsors/brand-logo";
import {
  sponsors,
  sponsorsSection,
  type HomeSponsor,
} from "@/components/brand/home/home-content";
import { Reveal, SECTION_CONTAINER, SectionHeading } from "@/components/brand/home/section";
import { cn } from "@/lib/utils";

/**
 * 赞助商跑马灯。
 *
 * 四个决定值得写下来：
 *
 * 1. **图形标彩色、字标单色。** 这不是折中，是这些品牌自己的横向锁定版式。
 *    整排全上色会让二十几套配色互相打架，谁也读不出来；全去色则等于
 *    把品牌辨识度最高的那部分抹掉。
 * 2. **跑马灯外面套一条 `card` 色带。** 用 `card` 而不是 `background` 是有原因的：
 *    这套主题的浅色档里 `--muted` 与 `--background` 是同一个色值（都是 `#f4f4f5`），
 *    分区底色 `muted/30` 叠在 `background` 上算出来还是它自己，`background` 的带子
 *    在浅色模式下完全看不见。`--card` 是浅色档里唯一与之有对比的中性面（`#ffffff`），
 *    深色档里它也比 `background` 亮一档，两边都成立。
 *    带子还顺带解决了边缘渐变的取色：`MarqueeFade` 的底色必须与身下的底色**完全**一致，
 *    而半透明的 `muted/30` 合成出来的颜色，用任何单一色值都对不上。
 * 3. **边缘处理是「渐变 + 模糊」两层。** 只盖一层同色渐变的话，logo 会在完全清晰的
 *    状态下突然被色块吞掉；加了 backdrop 模糊之后是先虚化再淡出。模糊本身也必须
 *    跟着 mask 一起衰减，不衰减就会在渐变结束的位置留下一条"清晰度突变"的竖线。
 * 4. **三行逐行反向。** 同向多行会被读成一整块在平移；反向才看得出这是一份在滚动的名单。
 *    行速也各不相同，速度一致的话反向的两行会保持固定相对位移，同样露出"整块"的破绽。
 *
 * `prefers-reduced-motion` 下直接停住（`play={false}`），名单仍然完整可读，
 * 与首页其它分区的动效约束一致。
 */

/**
 * 两侧边缘：实色打底 + backdrop 模糊，整体由 mask 线性衰减到全透明。
 *
 * 遮罩写成 arbitrary property 而不是 Tailwind 的 `mask-r-from-*`：后者会展开成
 * 六层 `mask-image` 再用 `mask-composite: intersect` 合成，在 Chromium 上合成结果
 * 恒为全透明，整个边缘层直接消失（表现是"渐变没生效"，而不是报错）。
 * 单层遮罩没有合成这一步。
 */
const FADE = "w-12 bg-none bg-card backdrop-blur-[3px] sm:w-28 lg:w-44";
/** 前 30% 保持不透明再开始衰减：纯线性的话最边上那个 logo 仍有一半可读，像是被裁掉而不是淡出 */
const FADE_MASK = {
  left: "[mask-image:linear-gradient(to_right,#000_30%,transparent)]",
  right: "[mask-image:linear-gradient(to_left,#000_30%,transparent)]",
} as const;

/**
 * 每行的高度，跑马灯容器与条目必须取同一个值。
 *
 * react-fast-marquee 在挂载前返回 null（它靠测量容器宽度决定复制几份），
 * 于是三行在首屏都是零高度、水合后才撑开 —— 不占位的话页面会在那一刻整体上跳。
 */
const ROW_HEIGHT = "h-14 sm:h-16";

const ROW_COUNT = 3;
const perRow = Math.ceil(sponsors.length / ROW_COUNT);
const rows = Array.from({ length: ROW_COUNT }, (_, index) =>
  sponsors.slice(index * perRow, (index + 1) * perRow)
).filter((row) => row.length > 0);

/** 逐行错开，避免相邻两行保持固定相对位移 */
const ROW_SPEED = [32, 26, 38];

function SponsorChip({ sponsor }: { sponsor: HomeSponsor }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Item
          asChild
          size="sm"
          className={cn(
            ROW_HEIGHT,
            "w-auto rounded-2xl px-6 text-foreground ring-border transition-all sm:px-9",
            "hover:ring-1"
          )}
        >
          <a href={sponsor.href} target="_blank" rel="noreferrer">
            <BrandLogo slug={sponsor.slug} className="text-[21px] sm:text-[25px]" />
          </a>
        </Item>
      </TooltipTrigger>
      <TooltipContent side="bottom" className="max-w-56 text-center">
        <span className="font-medium">{sponsor.name}</span>
        <span className="mt-0.5 block opacity-80">{sponsor.role ?? sponsor.category}</span>
      </TooltipContent>
    </Tooltip>
  );
}

export function SponsorsSection() {
  const reduced = useReducedMotion();

  return (
    <section id="sponsors" className="scroll-mt-20 border-t bg-muted/30 py-16 md:py-24">
      <div className={SECTION_CONTAINER}>
        <SectionHeading
          eyebrow={sponsorsSection.eyebrow}
          title={sponsorsSection.title}
          description={sponsorsSection.description}
          align="center"
        />
      </div>

      {/* 整幅出血：这一排的意思是"还在往两边延伸"，收进 max-w-7xl 后
          左右各留一段被容器边界切断的硬边，那反而在说"就这么多"。 */}
      <Reveal className="mt-12 border-y bg-card py-6 sm:py-8">
        <div className="flex flex-col gap-3 sm:gap-4">
          <TooltipProvider delayDuration={200}>
            {rows.map((row, index) => (
              <Marquee key={index} className={ROW_HEIGHT}>
                <MarqueeFade side="left" className={cn(FADE, FADE_MASK.left)} />
                <MarqueeFade side="right" className={cn(FADE, FADE_MASK.right)} />
                <MarqueeContent
                  direction={index % 2 === 0 ? "left" : "right"}
                  speed={ROW_SPEED[index] ?? 30}
                  play={!reduced}
                >
                  {row.map((sponsor) => (
                    <MarqueeItem key={sponsor.slug} className="mx-0">
                      <SponsorChip sponsor={sponsor} />
                    </MarqueeItem>
                  ))}
                </MarqueeContent>
              </Marquee>
            ))}
          </TooltipProvider>
        </div>
      </Reveal>

      <div className={cn(SECTION_CONTAINER, "mt-8")}>
        <p className="text-center text-xs text-muted-foreground">{sponsorsSection.disclaimer}</p>
      </div>
    </section>
  );
}
