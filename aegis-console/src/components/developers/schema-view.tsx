"use client";

import { useState } from "react";
import { ChevronRight } from "lucide-react";
import type { OpenAPISchema } from "@/lib/api/openapi";
import { cn } from "@/lib/utils";

/**
 * 递归展开的 schema 视图。
 *
 * 原来的实现只给一行 `object{a, b, …+3}`，嵌套字段全看不到，
 * 想知道响应里到底有什么只能自己发一次请求。这里把结构完整摊开，
 * 默认展开两层，再深的按需点开。
 */

function typeLabel(schema: OpenAPISchema): string {
  if (schema.enum?.length) return "enum";
  if (schema.type === "array") {
    const inner = schema.items ? typeLabel(schema.items) : "any";
    return `${inner}[]`;
  }
  if (schema.format) return `${schema.type || "string"}<${schema.format}>`;
  return schema.type || "any";
}

function SchemaRow({
  name,
  schema,
  required,
  depth,
  defaultOpen
}: {
  name?: string;
  schema: OpenAPISchema;
  required?: boolean;
  depth: number;
  defaultOpen: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);

  // 数组的字段结构挂在 items 上，展开时要穿透一层
  const target = schema.type === "array" && schema.items ? schema.items : schema;
  const properties = target.properties || {};
  const childNames = Object.keys(properties);
  const expandable = childNames.length > 0;
  const requiredSet = new Set(target.required || []);

  return (
    <div className={cn(depth > 0 && "border-l pl-3")}>
      <div className="flex items-baseline gap-2 py-1">
        {expandable ? (
          <button
            type="button"
            onClick={() => setOpen((value) => !value)}
            aria-expanded={open}
            aria-label={open ? "折叠" : "展开"}
            className="-ml-1 shrink-0 rounded p-0.5 text-muted-foreground hover:bg-muted"
          >
            <ChevronRight className={cn("size-3.5 transition-transform", open && "rotate-90")} />
          </button>
        ) : (
          <span className="w-[18px] shrink-0" />
        )}

        {name ? <code className="font-mono text-[12.5px]">{name}</code> : null}
        <span className="font-mono text-[11.5px] text-muted-foreground">{typeLabel(schema)}</span>
        {required ? (
          <span className="text-[11px] text-amber-600 dark:text-amber-400">必填</span>
        ) : null}
        {schema.enum?.length ? (
          <span className="truncate font-mono text-[11px] text-muted-foreground">
            {schema.enum.map((item) => JSON.stringify(item)).join(" | ")}
          </span>
        ) : null}
        {schema.description ? (
          <span className="min-w-0 flex-1 truncate text-[12px] text-muted-foreground">
            {schema.description}
          </span>
        ) : null}
      </div>

      {expandable && open ? (
        <div className="ml-[9px]">
          {childNames.map((child) => (
            <SchemaRow
              key={child}
              name={child}
              schema={properties[child]}
              required={requiredSet.has(child)}
              depth={depth + 1}
              defaultOpen={depth + 1 < 2}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}

export function SchemaView({ schema }: { schema?: OpenAPISchema }) {
  if (!schema) {
    return <p className="text-[12.5px] text-muted-foreground">无结构定义</p>;
  }
  const target = schema.type === "array" && schema.items ? schema.items : schema;
  if (!target.properties || !Object.keys(target.properties).length) {
    return (
      <p className="font-mono text-[12.5px] text-muted-foreground">{typeLabel(schema)}</p>
    );
  }
  return (
    <div className="rounded-lg border bg-background px-3 py-1.5">
      <SchemaRow schema={schema} depth={0} defaultOpen />
    </div>
  );
}
