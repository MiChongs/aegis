import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { LegalDocumentView } from "@/components/legal/legal-document-view";
import type { LegalDocType } from "@/lib/api/legal";

const DOC_TYPES: LegalDocType[] = ["terms", "privacy"];

type PageProps = {
  params: Promise<{ docType: string }>;
  searchParams: Promise<{ locale?: string }>;
};

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { docType } = await params;
  // 标题不在这里查接口：generateMetadata 跑在服务端，而正文是浏览器带着
  // Accept-Language 去取的（语言协商依赖那个头）。两边各查一次会协商出不同语言，
  // 表现为「标签页标题是英文、正文是中文」。
  const title = docType === "privacy" ? "隐私政策 Privacy Policy" : "用户服务协议 Terms of Service";
  return { title: `${title} · Aegis` };
}

/**
 * 法律文本页。
 *
 * 取代了原来的对话框。对话框有三个问题，都不是样式能解决的：
 *   1. **没有地址**。分享不出去、收藏不了、搜索引擎看不到，而法律文本恰恰
 *      需要一个稳定的公示地址。
 *   2. **读到一半会丢**。在注册表单上点开条款，关掉对话框才能继续填；
 *      条款有一万多字，滚动读到第十节时一个 Esc 就回到起点。
 *   3. **装不下**。`max-h-[85vh]` 的滚动区里塞一份完整协议，目录、锚点、
 *      打印这些长文档该有的东西一个都做不了。
 *
 * 现在是一个独立路由，从表单上以新标签打开 —— 已经填好的内容不会因此丢失。
 */
export default async function LegalPage({ params, searchParams }: PageProps) {
  const { docType } = await params;
  if (!DOC_TYPES.includes(docType as LegalDocType)) {
    notFound();
  }
  const { locale } = await searchParams;

  return <LegalDocumentView docType={docType as LegalDocType} initialLocale={locale} />;
}
