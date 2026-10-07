import { cn } from "@/lib/utils";

const METHOD_STYLES: Record<string, string> = {
  GET: "bg-sky-500/12 text-sky-700 ring-sky-500/25 dark:text-sky-300",
  POST: "bg-emerald-500/12 text-emerald-700 ring-emerald-500/25 dark:text-emerald-300",
  PUT: "bg-amber-500/14 text-amber-700 ring-amber-500/25 dark:text-amber-300",
  PATCH: "bg-violet-500/12 text-violet-700 ring-violet-500/25 dark:text-violet-300",
  DELETE: "bg-red-500/12 text-red-700 ring-red-500/25 dark:text-red-300"
};

/** 文字色，用于只需要方法名着色、不需要底色的紧凑场景（侧栏列表） */
const METHOD_TEXT: Record<string, string> = {
  GET: "text-sky-600 dark:text-sky-400",
  POST: "text-emerald-600 dark:text-emerald-400",
  PUT: "text-amber-600 dark:text-amber-400",
  PATCH: "text-violet-600 dark:text-violet-400",
  DELETE: "text-red-600 dark:text-red-400"
};

export function MethodBadge({
  method,
  size = "sm",
  className
}: {
  method: string;
  size?: "sm" | "md";
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center justify-center rounded-md font-mono font-bold tracking-wide ring-1 ring-inset",
        size === "md" ? "h-7 min-w-[64px] px-2 text-[11.5px]" : "h-5 w-[52px] text-[10px]",
        METHOD_STYLES[method] || "bg-muted text-muted-foreground ring-border",
        className
      )}
    >
      {method}
    </span>
  );
}

export function MethodText({ method, className }: { method: string; className?: string }) {
  return (
    <span
      className={cn(
        "w-[46px] shrink-0 font-mono text-[10px] font-bold tracking-wide",
        METHOD_TEXT[method] || "text-muted-foreground",
        className
      )}
    >
      {method === "DELETE" ? "DEL" : method}
    </span>
  );
}

/** 路径里的 `{param}` 段单独着色，读路径时一眼看出哪里需要替换 */
export function PathText({ path, className }: { path: string; className?: string }) {
  const parts = path.split(/(\{[^}]+\})/g).filter(Boolean);
  return (
    <span className={cn("font-mono", className)}>
      {parts.map((part, index) =>
        part.startsWith("{") ? (
          <span key={index} className="text-primary">
            {part}
          </span>
        ) : (
          <span key={index}>{part}</span>
        )
      )}
    </span>
  );
}
