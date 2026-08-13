"use client";

import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { CodeBlock } from "@/components/developers/code-block";
import { securityLevels } from "@/components/brand/home/home-content";
import { Reveal, Section, SectionHeading } from "@/components/brand/home/section";

/** 各档示例代码用的语法（决定高亮方案，与档位一一对应） */
const LEVEL_LANGUAGE: Record<string, string> = {
  standard: "bash",
  signed: "kotlin",
  sealed: "http",
};

/**
 * 接入。
 *
 * 三档安全等级摆在同一个 Tabs 里，是为了让"它们共用同一批路径与同一份 JSON"
 * 这句话能被直接看出来 —— 分成三段各讲一遍，读者反而会以为是三套协议。
 */
export function IntegrationSection() {
  return (
    <Section id="integration" bordered className="bg-muted/30">
      <SectionHeading
        eyebrow="接入"
        title="单一命名空间，三档安全等级"
        description="客户端登录后所需的全部能力均位于 /api/v1/apps/{appKey}/* 之下。三档等级为叠加关系，sealed 等同于 signed 叠加加密载荷，升级仅需替换传输层适配器。"
      />

      <Reveal className="mt-10">
        <Tabs defaultValue={securityLevels[0]!.key}>
          <TabsList className="w-full max-w-md">
            {securityLevels.map((level) => (
              <TabsTrigger key={level.key} value={level.key} className="font-mono">
                {level.label}
              </TabsTrigger>
            ))}
          </TabsList>

          {securityLevels.map((level) => (
            <TabsContent
              key={level.key}
              value={level.key}
              className="mt-6 grid gap-6 lg:grid-cols-[minmax(0,20rem)_minmax(0,1fr)] lg:gap-10"
            >
              <div className="flex flex-col gap-4">
                <Badge variant="secondary" className="w-fit font-normal">
                  {level.tagline}
                </Badge>
                <p className="text-sm leading-relaxed text-pretty text-muted-foreground">
                  {level.description}
                </p>
                <p className="rounded-md border border-dashed bg-background px-3 py-2 text-xs text-muted-foreground">
                  {level.requirement}
                </p>
              </div>

              <CodeBlock
                code={level.code}
                language={LEVEL_LANGUAGE[level.key] ?? "bash"}
                title={`${level.label} · 登录`}
                className="bg-background"
              />
            </TabsContent>
          ))}
        </Tabs>
      </Reveal>

      {/* 这两个是同一件事的两个去处（读规格 / 查接口），
          因此用 ButtonGroup 粘成一个分段控件 —— 与首屏那对并列 CTA 不同。 */}
      <Reveal delay={0.1} className="mt-8">
        <ButtonGroup>
          <Button asChild variant="outline">
            <Link href="/developers">
              完整接入文档
              <ArrowUpRight />
            </Link>
          </Button>
          <Button asChild variant="outline">
            <Link href="/developers/api">
              浏览全部接口
              <ArrowUpRight />
            </Link>
          </Button>
        </ButtonGroup>
      </Reveal>
    </Section>
  );
}
