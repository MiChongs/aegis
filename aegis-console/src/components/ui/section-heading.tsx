import { ReactNode } from "react";

export function SectionHeading({
  eyebrow,
  title,
  action
}: {
  eyebrow?: string;
  title: string;
  description?: string; // 保留签名兼容但不渲染
  action?: ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div className="flex items-baseline gap-2">
        {eyebrow ? <span className="text-xs text-muted-foreground">{eyebrow}</span> : null}
        {eyebrow ? <span className="text-xs text-muted-foreground">/</span> : null}
        <h1 className="text-lg font-semibold tracking-tight text-foreground">{title}</h1>
      </div>
      {action ? <div className="flex items-center gap-2">{action}</div> : null}
    </div>
  );
}
