"use client";

import {
  Item,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemMedia,
  ItemTitle,
} from "@/components/ui/item";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { capabilityGroups } from "@/components/brand/home/home-content";
import { Reveal, Section, SectionHeading } from "@/components/brand/home/section";
import { Grain, Pattern, SpotlightCard } from "@/components/brand/home/visuals";

/**
 * 能力全景。
 *
 * 三十多条能力平铺成一张网格，读者会在第八条放弃；分成六组之后，
 * 他能先认出哪一组是自己要的，再去读那六条。窄屏上标签行自己横向滚动，
 * 而不是换行把标题顶走。
 */
export function CapabilitiesSection() {
  return (
    <Section id="capabilities" bordered className="relative overflow-hidden bg-muted/30">
      <Pattern
        size={64}
        mask="radial-gradient(ellipse 65% 55% at 50% 45%, #000, transparent 82%)"
      />
      <Grain />

      <div className="relative">
        <SectionHeading
          eyebrow="能力全景"
          title="六组能力，覆盖账号的完整生命周期"
          description="按使用场景分组编排。每一项在管理控制台中均有对应页面，并提供对应的开放接口。"
        />

        <Reveal className="mt-10">
          <Tabs defaultValue={capabilityGroups[0]!.key}>
            {/* 用默认的填充式页签而不是 line 变体：后者的选中下划线画在
                trigger 之外（bottom: -5px），放进横向滚动容器里会被裁掉。 */}
            <div className="overflow-x-auto">
              <TabsList className="w-max bg-background/70 backdrop-blur">
                {capabilityGroups.map((group) => (
                  <TabsTrigger key={group.key} value={group.key} className="px-3">
                    <group.icon aria-hidden />
                    {group.label}
                  </TabsTrigger>
                ))}
              </TabsList>
            </div>

            {capabilityGroups.map((group) => (
              <TabsContent key={group.key} value={group.key} className="mt-6">
                <p className="mb-4 max-w-2xl text-sm text-muted-foreground">{group.summary}</p>
                <ItemGroup className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
                  {group.items.map((item) => (
                    <SpotlightCard key={item.title} className="rounded-md">
                      <Item
                        variant="outline"
                        className="h-full items-start bg-card/80 transition-colors duration-300 hover:border-[color-mix(in_srgb,var(--home-accent)_38%,var(--border))]"
                      >
                        <ItemMedia
                          variant="icon"
                          className="border-[color-mix(in_srgb,var(--home-accent)_24%,var(--border))] bg-[color-mix(in_srgb,var(--home-accent)_10%,transparent)]"
                        >
                          <item.icon strokeWidth={1.75} style={{ color: "var(--home-accent)" }} />
                        </ItemMedia>
                        <ItemContent>
                          <ItemTitle>{item.title}</ItemTitle>
                          <ItemDescription className="line-clamp-none">
                            {item.description}
                          </ItemDescription>
                        </ItemContent>
                      </Item>
                    </SpotlightCard>
                  ))}
                </ItemGroup>
              </TabsContent>
            ))}
          </Tabs>
        </Reveal>
      </div>
    </Section>
  );
}
