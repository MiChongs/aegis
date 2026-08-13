"use client";

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { faqs } from "@/components/brand/home/home-content";
import { Reveal, Section, SectionHeading } from "@/components/brand/home/section";

/**
 * 常见问题。
 *
 * 收录标准只有一条：**这个问题不回答，读者就无法判断要不要继续往下走**。
 * 「支持哪些语言的 SDK」这类查一下就知道的事不放这里，它属于文档。
 */
export function FaqSection() {
  return (
    <Section id="faq" bordered>
      <div className="grid gap-10 lg:grid-cols-[minmax(0,20rem)_minmax(0,1fr)] lg:gap-16">
        <div className="lg:sticky lg:top-24 lg:self-start">
          <SectionHeading eyebrow="常见问题" title="接入前的常见疑问" />
        </div>

        <Reveal>
          <Accordion type="single" collapsible className="w-full">
            {faqs.map((faq) => (
              <AccordionItem key={faq.question} value={faq.question}>
                <AccordionTrigger className="text-base hover:no-underline">
                  {faq.question}
                </AccordionTrigger>
                <AccordionContent className="max-w-3xl text-sm leading-relaxed text-pretty text-muted-foreground">
                  {faq.answer}
                </AccordionContent>
              </AccordionItem>
            ))}
          </Accordion>
        </Reveal>
      </div>
    </Section>
  );
}
