import Link from "next/link";
import { cn } from "@/lib/utils";

/**
 * 登录 / 注册表单下方的条款链接。
 *
 * **在新标签打开**（`target="_blank"`）。这不是审美选择：注册表单上已经填了
 * 账号、密码、确认密码，点一下条款就把整页导航走，回来时表单是空的 ——
 * 而条款恰恰是这个页面上唯一鼓励用户点开的东西。
 *
 * 文案不写死在这里：链接的**标题**由目标页面从服务端取，这里只写「用户协议」
 * 这四个字作为入口标签。整份文本在 `/legal/[docType]`。
 */
export function LegalLinks({
  className,
  prefix = "登录即表示同意",
}: {
  className?: string;
  prefix?: string;
}) {
  return (
    <p className={cn("text-[11px] leading-5 text-muted-foreground/70", className)}>
      {prefix}{" "}
      <Link
        href="/legal/terms"
        target="_blank"
        rel="noopener noreferrer"
        className="underline underline-offset-2 transition-colors hover:text-foreground"
      >
        用户协议
      </Link>{" "}
      与{" "}
      <Link
        href="/legal/privacy"
        target="_blank"
        rel="noopener noreferrer"
        className="underline underline-offset-2 transition-colors hover:text-foreground"
      >
        隐私政策
      </Link>
    </p>
  );
}
