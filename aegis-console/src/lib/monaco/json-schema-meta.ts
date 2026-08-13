/**
 * 给「入参契约」编辑器用的 JSON Schema 元 schema。
 *
 * 为什么不直接用官方那份 draft-07 元 schema：它是一个 `$ref` 满天飞的
 * 自引用文档，Monaco 的 JSON 服务能校验它，但补全体验很差（在 `properties`
 * 下面按补全会列出 200 多个关键字，其中绝大多数我们的生成器根本不认）。
 *
 * 这里只登记**服务端真的会处理**的那一批关键字，因此补全列表里出现什么，
 * 生成出来的 TypeScript 类型就会体现什么。这与「能力目录只列真的绑定了的能力」
 * 是同一条取舍：一个能补全出来、却不起作用的关键字，比补不出来更误导人。
 *
 * `enableSchemaRequest` 是关掉的，所以这份内容必须内联在包里 ——
 * 不能写成一个 $ref 指向 json-schema.org，那个请求发不出去。
 */

const TYPE_NAMES = ["string", "number", "integer", "boolean", "object", "array", "null"];

/** 字段级 schema：递归引用自身以支持嵌套对象与数组元素 */
const fieldSchema = {
  type: "object",
  markdownDescription: "一个字段的约束。生成 TypeScript 类型时认得的关键字都在补全列表里。",
  properties: {
    type: {
      markdownDescription:
        "字段类型。`integer` 与 `number` 都会生成 TS 的 `number`；" +
        "数组形式（如 `[\"string\",\"null\"]`）取第一个非 null 项。",
      anyOf: [
        { type: "string", enum: TYPE_NAMES },
        { type: "array", items: { type: "string", enum: TYPE_NAMES } }
      ]
    },
    description: {
      type: "string",
      markdownDescription: "字段说明。会作为 JSDoc 出现在编辑器的补全与悬浮里。"
    },
    enum: {
      type: "array",
      markdownDescription:
        "可取值。会生成**字面量联合类型**（`\"web\" | \"ios\"`），" +
        "比 `type: string` 精确得多 —— 拼错值当场标红。"
    },
    default: { markdownDescription: "默认值，写进 JSDoc。注意服务端不会替你填它。" },
    minLength: { type: "integer", minimum: 0, description: "字符串最短长度" },
    maxLength: { type: "integer", minimum: 0, description: "字符串最长长度" },
    pattern: { type: "string", description: "正则约束（ECMA-262 语法）" },
    format: {
      type: "string",
      description: "语义格式。仅作校验提示，不影响生成的 TS 类型。",
      enum: ["email", "uri", "uuid", "date", "date-time", "ipv4", "ipv6", "hostname"]
    },
    minimum: { type: "number", description: "数值下界（含）" },
    maximum: { type: "number", description: "数值上界（含）" },
    multipleOf: { type: "number", exclusiveMinimum: 0, description: "必须是它的整数倍" },
    minItems: { type: "integer", minimum: 0, description: "数组最少元素数" },
    maxItems: { type: "integer", minimum: 0, description: "数组最多元素数" },
    uniqueItems: { type: "boolean", description: "数组元素是否必须互不相同" },
    items: {
      $ref: "#/definitions/field",
      markdownDescription: "数组元素的约束。生成 `T[]`。"
    },
    properties: {
      type: "object",
      markdownDescription: "嵌套对象的字段表。会展开成内联的 TS 对象类型。",
      additionalProperties: { $ref: "#/definitions/field" }
    },
    required: {
      type: "array",
      items: { type: "string" },
      markdownDescription: "嵌套对象里的必填字段。**不在这个列表里的字段生成为可选**（带 `?`）。"
    },
    additionalProperties: {
      markdownDescription:
        "是否允许未声明的字段。设为 `false` 时生成的类型是封闭的，" +
        "读一个拼错的字段名会当场标红；不写等同于 `false`。",
      anyOf: [{ type: "boolean" }, { $ref: "#/definitions/field" }]
    },
    oneOf: {
      type: "array",
      items: { $ref: "#/definitions/field" },
      markdownDescription: "多选一。会生成联合类型；分支里有认不出的构造时整体降级成 `any`。"
    },
    anyOf: {
      type: "array",
      items: { $ref: "#/definitions/field" },
      markdownDescription: "任意匹配。生成方式同 `oneOf`。"
    }
  }
} as const;

export const INPUT_SCHEMA_META = {
  $schema: "http://json-schema.org/draft-07/schema#",
  title: "Aegis 远程函数入参契约",
  markdownDescription:
    "这份声明同时驱动三件事：调用入口的前置校验、试跑输入框的补全与校验、" +
    "以及编辑器里 `ctx.input` 的真实类型。留空（`{}`）表示不约束。",
  type: "object",
  definitions: { field: fieldSchema },
  properties: {
    type: {
      type: "string",
      enum: ["object"],
      markdownDescription: "顶层必须是 `object` —— 调用体的 `input` 字段就是一个对象。"
    },
    description: { type: "string", description: "这个函数收什么，一句话说明" },
    required: {
      type: "array",
      items: { type: "string" },
      markdownDescription:
        "必填字段。**不在这里的字段在生成的类型里带 `?`**，" +
        "脚本里读它之前要判空。"
    },
    additionalProperties: {
      type: "boolean",
      markdownDescription:
        "建议设为 `false`：调用方多传一个字段时当场拒绝，" +
        "而不是让一个拼错的字段名静默地什么都不做。"
    },
    properties: {
      type: "object",
      markdownDescription: "字段表。键就是 `ctx.input` 上的属性名。",
      additionalProperties: { $ref: "#/definitions/field" }
    }
  }
} as Record<string, unknown>;
