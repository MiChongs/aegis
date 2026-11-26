"use client";

import { useMemo, useState } from "react";
import { ArrowDown, ArrowUp, FolderTree, ListPlus, Pencil, Plus, Save, X } from "lucide-react";
import { notify } from "@/lib/notify";
import {
  useDeleteTicketCategoryMutation,
  useSaveTicketCategoryMutation,
  useTicketCategoriesQuery,
  useTicketGroupsQuery,
  useTicketSLAPoliciesQuery
} from "@/lib/ticket-hooks";
import type { TicketCategory, TicketFormField, TicketKind, TicketPriority } from "@/lib/api/tickets";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { KIND_LABEL, PRIORITY_LABEL } from "../ticket-shared";
import {
  DeleteButton,
  EntityGroupLabel,
  EntityList,
  Field,
  KINDS,
  ListSkeleton,
  PRIORITIES,
  PRIORITY_TONE,
  ScopeBadges,
  SectionEmpty,
  SectionError,
  SectionHeader,
  SwitchRow,
  reportError
} from "./settings-shared";
import { cn } from "@/lib/utils";

// 分类：按「工单 / 意见反馈」分两组列出。
// 一行说清名称、标识、默认优先级与默认去向，编辑在右侧抽屉里完成（含提单表单的自定义字段）。

const FIELD_TYPES: { value: string; label: string }[] = [
  { value: "text", label: "单行文本" },
  { value: "textarea", label: "多行文本" },
  { value: "select", label: "下拉选择" },
  { value: "number", label: "数字" },
  { value: "date", label: "日期" },
  { value: "switch", label: "开关" }
];
const FIELD_TYPE_LABEL = Object.fromEntries(FIELD_TYPES.map((item) => [item.value, item.label]));

type KindFilter = "all" | TicketKind;

type CategoryForm = {
  kind: TicketKind;
  parentId: string;
  key: string;
  name: string;
  description: string;
  defaultPriority: TicketPriority;
  defaultGroupId: string;
  slaPolicyId: string;
  userSubmittable: boolean;
  sort: number;
  enabled: boolean;
  formSchema: TicketFormField[];
};

function emptyForm(kind: TicketKind): CategoryForm {
  return {
    kind,
    parentId: "none",
    key: "",
    name: "",
    description: "",
    defaultPriority: "normal",
    defaultGroupId: "none",
    slaPolicyId: "none",
    userSubmittable: true,
    sort: 0,
    enabled: true,
    formSchema: []
  };
}

function formFromCategory(item: TicketCategory): CategoryForm {
  return {
    kind: item.kind ?? "ticket",
    parentId: item.parentId ? String(item.parentId) : "none",
    key: item.key,
    name: item.name,
    description: item.description,
    defaultPriority: item.defaultPriority,
    defaultGroupId: item.defaultGroupId ? String(item.defaultGroupId) : "none",
    slaPolicyId: item.slaPolicyId ? String(item.slaPolicyId) : "none",
    userSubmittable: item.userSubmittable,
    sort: item.sort,
    enabled: item.enabled,
    formSchema: (item.formSchema ?? []).map((field) => ({ ...field, options: field.options ? [...field.options] : undefined }))
  };
}

function optionalId(value: string) {
  return value === "none" ? undefined : Number(value);
}

export function CategorySection({ appId }: { appId: number }) {
  const query = useTicketCategoriesQuery(appId);
  const groupsQuery = useTicketGroupsQuery(appId);
  const slaQuery = useTicketSLAPoliciesQuery(appId);
  const saveMut = useSaveTicketCategoryMutation();
  const deleteMut = useDeleteTicketCategoryMutation();

  const [filter, setFilter] = useState<KindFilter>("all");
  const [editing, setEditing] = useState<{ id?: number; form: CategoryForm } | null>(null);

  const categories = useMemo(() => [...(query.data ?? [])].sort((a, b) => a.sort - b.sort || a.id - b.id), [query.data]);
  const groupName = useMemo(
    () => new Map((groupsQuery.data ?? []).map((group) => [group.id, group.name])),
    [groupsQuery.data]
  );
  const slaName = useMemo(() => new Map((slaQuery.data ?? []).map((policy) => [policy.id, policy.name])), [slaQuery.data]);
  const nameById = useMemo(() => new Map(categories.map((item) => [item.id, item.name])), [categories]);

  const buckets = KINDS.filter((kind) => filter === "all" || filter === kind).map((kind) => ({
    kind,
    items: categories.filter((item) => (item.kind ?? "ticket") === kind)
  }));

  const save = async (id: number | undefined, form: CategoryForm) => {
    if (!form.key.trim() || !form.name.trim()) {
      notify.warning("请填写分类标识与名称");
      return false;
    }
    const badField = form.formSchema.find((field) => !field.key.trim() || !field.label.trim());
    if (badField) {
      notify.warning("表单字段的标识与名称都要填写");
      return false;
    }
    const keys = form.formSchema.map((field) => field.key.trim());
    if (new Set(keys).size !== keys.length) {
      notify.warning("表单字段的标识不能重复");
      return false;
    }
    try {
      await saveMut.mutateAsync({
        id,
        payload: {
          appid: appId,
          kind: form.kind,
          parentId: optionalId(form.parentId),
          key: form.key.trim(),
          name: form.name.trim(),
          description: form.description.trim(),
          defaultPriority: form.defaultPriority,
          defaultGroupId: optionalId(form.defaultGroupId),
          slaPolicyId: optionalId(form.slaPolicyId),
          userSubmittable: form.userSubmittable,
          sort: Number(form.sort) || 0,
          enabled: form.enabled,
          formSchema: form.formSchema.map((field) => ({
            ...field,
            key: field.key.trim(),
            label: field.label.trim(),
            placeholder: field.placeholder?.trim() || undefined,
            options: field.type === "select" ? (field.options ?? []).map((option) => option.trim()).filter(Boolean) : undefined
          }))
        }
      });
      notify.success("分类已保存");
      return true;
    } catch (error) {
      reportError(error, "保存失败");
      return false;
    }
  };

  const toggleEnabled = async (item: TicketCategory, enabled: boolean) => {
    await save(item.id, { ...formFromCategory(item), enabled });
  };

  return (
    <div className="space-y-4">
      <SectionHeader
        title="分类"
        description="用户提交时选择的问题类型，决定默认优先级、默认处理组与 SLA。"
        action={
          <>
            <Tabs value={filter} onValueChange={(value) => setFilter(value as KindFilter)}>
              <TabsList className="h-8">
                <TabsTrigger value="all" className="text-xs">
                  全部
                </TabsTrigger>
                <TabsTrigger value="ticket" className="text-xs">
                  工单
                </TabsTrigger>
                <TabsTrigger value="feedback" className="text-xs">
                  意见反馈
                </TabsTrigger>
              </TabsList>
            </Tabs>
            <Button size="sm" onClick={() => setEditing({ form: emptyForm(filter === "feedback" ? "feedback" : "ticket") })}>
              <Plus />
              新建分类
            </Button>
          </>
        }
      />

      {query.isLoading ? (
        <ListSkeleton />
      ) : query.isError ? (
        <SectionError error={query.error} onRetry={() => void query.refetch()} />
      ) : categories.length === 0 ? (
        <SectionEmpty
          icon={FolderTree}
          title="还没有分类"
          description="没有分类时，用户提交不需要选择类型，全部按普通优先级进入待受理。"
          action={
            <Button size="sm" onClick={() => setEditing({ form: emptyForm("ticket") })}>
              <Plus />
              新建分类
            </Button>
          }
        />
      ) : (
        <div className="space-y-4">
          {buckets.map(({ kind, items }) => (
            <div key={kind}>
              <EntityGroupLabel count={items.length}>{KIND_LABEL[kind]}</EntityGroupLabel>
              {items.length === 0 ? (
                <div className="rounded-xl border border-dashed px-4 py-5 text-center text-sm text-muted-foreground">
                  暂无{KIND_LABEL[kind]}分类
                </div>
              ) : (
                <EntityList>
                  {items.map((item) => (
                    <div
                      key={item.id}
                      className={cn("group flex items-center gap-3 px-4 py-3", !item.enabled && "bg-muted/30")}
                    >
                      <span className={cn("size-2 shrink-0 rounded-full", PRIORITY_TONE[item.defaultPriority])} />
                      <div className="min-w-0 flex-1 space-y-0.5">
                        <div className="flex flex-wrap items-center gap-1.5">
                          {item.parentId && nameById.get(item.parentId) ? (
                            <span className="text-sm text-muted-foreground">{nameById.get(item.parentId)} /</span>
                          ) : null}
                          <span className={cn("text-sm font-medium", item.enabled ? "text-foreground" : "text-muted-foreground")}>
                            {item.name}
                          </span>
                          <code className="rounded bg-muted px-1 py-px font-mono text-[10px] text-muted-foreground">
                            {item.key}
                          </code>
                          <ScopeBadges platform={item.appid === 0 && appId !== 0} />
                          {item.formSchema?.length ? (
                            <Badge variant="outline" size="sm">
                              {item.formSchema.length} 个字段
                            </Badge>
                          ) : null}
                        </div>
                        <p className="truncate text-xs text-muted-foreground">
                          {[
                            `默认${PRIORITY_LABEL[item.defaultPriority]}`,
                            item.defaultGroupId ? `进入 ${groupName.get(item.defaultGroupId) ?? "处理组"}` : "不指定处理组",
                            item.slaPolicyId ? `SLA ${slaName.get(item.slaPolicyId) ?? "已绑定"}` : null,
                            item.userSubmittable ? "用户可自助提交" : "仅管理员代提",
                            item.description || null
                          ]
                            .filter(Boolean)
                            .join(" · ")}
                        </p>
                      </div>
                      <span className="hidden w-10 text-right text-[11px] tabular-nums text-muted-foreground sm:block">
                        #{item.sort}
                      </span>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span>
                            <Switch
                              checked={item.enabled}
                              disabled={saveMut.isPending}
                              onCheckedChange={(value) => void toggleEnabled(item, value)}
                              aria-label={item.enabled ? "停用" : "启用"}
                            />
                          </span>
                        </TooltipTrigger>
                        <TooltipContent>{item.enabled ? "已启用" : "已停用"}</TooltipContent>
                      </Tooltip>
                      <Button
                        size="icon-sm"
                        variant="ghost"
                        aria-label="编辑"
                        onClick={() => setEditing({ id: item.id, form: formFromCategory(item) })}
                      >
                        <Pencil />
                      </Button>
                      <DeleteButton
                        title={`删除分类「${item.name}」？`}
                        description="删除后用户不能再选择这个分类，此操作不可撤销。"
                        pending={deleteMut.isPending}
                        onConfirm={async () => {
                          try {
                            await deleteMut.mutateAsync(item.id);
                            notify.success("分类已删除");
                          } catch (error) {
                            reportError(error, "删除失败");
                          }
                        }}
                      />
                    </div>
                  ))}
                </EntityList>
              )}
            </div>
          ))}
        </div>
      )}

      <CategorySheet
        key={editing ? `${editing.id ?? "new"}` : "closed"}
        editing={editing}
        onClose={() => setEditing(null)}
        categories={categories}
        groups={(groupsQuery.data ?? []).map((group) => ({ id: group.id, name: group.name }))}
        policies={(slaQuery.data ?? []).map((policy) => ({ id: policy.id, name: policy.name }))}
        pending={saveMut.isPending}
        onSave={async (form) => {
          if (await save(editing?.id, form)) setEditing(null);
        }}
      />
    </div>
  );
}

function CategorySheet({
  editing,
  onClose,
  categories,
  groups,
  policies,
  pending,
  onSave
}: {
  editing: { id?: number; form: CategoryForm } | null;
  onClose: () => void;
  categories: TicketCategory[];
  groups: { id: number; name: string }[];
  policies: { id: number; name: string }[];
  pending: boolean;
  onSave: (form: CategoryForm) => void;
}) {
  const [form, setForm] = useState<CategoryForm>(() => editing?.form ?? emptyForm("ticket"));
  const patch = (next: Partial<CategoryForm>) => setForm((prev) => ({ ...prev, ...next }));
  const parents = categories.filter(
    (item) => (item.kind ?? "ticket") === form.kind && item.id !== editing?.id && !item.parentId
  );

  return (
    <Sheet open={Boolean(editing)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="w-full gap-0 sm:max-w-xl">
        <SheetHeader className="border-b">
          <SheetTitle>{editing?.id ? "编辑分类" : "新建分类"}</SheetTitle>
          <SheetDescription>
            {form.kind === "feedback" ? "意见反馈分类出现在用户端的反馈入口。" : "工单只对管理员开放，工单分类只在后台新建工单时可选。"}
          </SheetDescription>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto p-4">
          <section className="space-y-3">
            <h3 className="text-sm font-medium text-foreground">基本信息</h3>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="类型">
                <Select value={form.kind} onValueChange={(value) => patch({ kind: value as TicketKind, parentId: "none" })}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {KINDS.map((kind) => (
                      <SelectItem key={kind} value={kind}>
                        {KIND_LABEL[kind]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="上级分类">
                <Select value={form.parentId} onValueChange={(value) => patch({ parentId: value })}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">无（一级分类）</SelectItem>
                    {parents.map((item) => (
                      <SelectItem key={item.id} value={String(item.id)}>
                        {item.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="名称">
                <Input value={form.name} onChange={(event) => patch({ name: event.target.value })} placeholder="支付问题" />
              </Field>
              <Field label="标识" hint="英文、数字与下划线，客户端按它识别分类">
                <Input
                  value={form.key}
                  onChange={(event) => patch({ key: event.target.value })}
                  placeholder="payment"
                  className="font-mono"
                />
              </Field>
            </div>
            <Field label="描述">
              <Textarea
                rows={2}
                value={form.description}
                onChange={(event) => patch({ description: event.target.value })}
                placeholder={form.kind === "feedback" ? "在用户端选择分类时显示" : "在后台新建工单选择分类时显示"}
              />
            </Field>
          </section>

          <section className="space-y-3">
            <h3 className="text-sm font-medium text-foreground">进入后的去向</h3>
            <Field label="默认优先级">
              <div className="grid grid-cols-4 gap-1.5">
                {PRIORITIES.map((priority) => (
                  <button
                    key={priority}
                    type="button"
                    onClick={() => patch({ defaultPriority: priority })}
                    className={cn(
                      "flex items-center justify-center gap-1.5 rounded-md border px-2 py-1.5 text-xs transition-colors",
                      form.defaultPriority === priority
                        ? "border-primary bg-primary/5 font-medium text-foreground"
                        : "text-muted-foreground hover:bg-accent"
                    )}
                  >
                    <span className={cn("size-1.5 rounded-full", PRIORITY_TONE[priority])} />
                    {PRIORITY_LABEL[priority]}
                  </button>
                ))}
              </div>
            </Field>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="默认处理组">
                <Select value={form.defaultGroupId} onValueChange={(value) => patch({ defaultGroupId: value })}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">不指定</SelectItem>
                    {groups.map((group) => (
                      <SelectItem key={group.id} value={String(group.id)}>
                        {group.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="SLA 策略">
                <Select value={form.slaPolicyId} onValueChange={(value) => patch({ slaPolicyId: value })}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">不指定</SelectItem>
                    {policies.map((policy) => (
                      <SelectItem key={policy.id} value={String(policy.id)}>
                        {policy.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
          </section>

          <section className="space-y-2">
            <h3 className="text-sm font-medium text-foreground">可用性</h3>
            <SwitchRow
              title="允许用户自助提交"
              description="关闭后只有管理员能代用户登记这一类"
              control={
                <Switch checked={form.userSubmittable} onCheckedChange={(value) => patch({ userSubmittable: value })} />
              }
            />
            <SwitchRow
              title="启用"
              description="停用后不再出现在选择列表里，已有数据不受影响"
              control={<Switch checked={form.enabled} onCheckedChange={(value) => patch({ enabled: value })} />}
            />
            <div className="flex items-center justify-between gap-4 rounded-lg border px-3 py-2.5">
              <div>
                <p className="text-sm text-foreground">排序</p>
                <p className="text-xs text-muted-foreground">数字小的排在前面</p>
              </div>
              <Input
                type="number"
                className="h-8 w-24"
                value={form.sort}
                onChange={(event) => patch({ sort: Number(event.target.value) })}
              />
            </div>
          </section>

          <FormSchemaEditor fields={form.formSchema} onChange={(formSchema) => patch({ formSchema })} />
        </div>

        <SheetFooter className="flex-row justify-end gap-2 border-t">
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button disabled={pending} onClick={() => onSave(form)}>
            <Save />
            保存
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

/** 提单表单的自定义字段：客户端按这份定义渲染输入项 */
function FormSchemaEditor({ fields, onChange }: { fields: TicketFormField[]; onChange: (fields: TicketFormField[]) => void }) {
  const update = (index: number, next: Partial<TicketFormField>) =>
    onChange(fields.map((field, i) => (i === index ? { ...field, ...next } : field)));
  const move = (index: number, delta: number) => {
    const target = index + delta;
    if (target < 0 || target >= fields.length) return;
    const next = [...fields];
    [next[index], next[target]] = [next[target], next[index]];
    onChange(next);
  };

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <div>
          <h3 className="text-sm font-medium text-foreground">表单字段</h3>
          <p className="text-xs text-muted-foreground">选了这个分类后，提交页额外要填的内容</p>
        </div>
        <Button
          size="sm"
          variant="outline"
          onClick={() => onChange([...fields, { key: "", label: "", type: "text", required: false }])}
        >
          <ListPlus />
          添加字段
        </Button>
      </div>

      {fields.length === 0 ? (
        <div className="rounded-lg border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">
          没有自定义字段，用户只需填写标题与描述
        </div>
      ) : (
        <div className="space-y-2">
          {fields.map((field, index) => (
            <div key={index} className="space-y-2 rounded-lg border bg-muted/20 p-3">
              <div className="flex items-center gap-1.5">
                <span className="text-xs font-medium text-muted-foreground">字段 {index + 1}</span>
                <Badge variant="outline" size="sm">
                  {FIELD_TYPE_LABEL[field.type] ?? field.type}
                </Badge>
                {field.required ? (
                  <Badge variant="warning" size="sm">
                    必填
                  </Badge>
                ) : null}
                <div className="ml-auto flex items-center">
                  <Button size="icon-sm" variant="ghost" aria-label="上移" disabled={index === 0} onClick={() => move(index, -1)}>
                    <ArrowUp />
                  </Button>
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    aria-label="下移"
                    disabled={index === fields.length - 1}
                    onClick={() => move(index, 1)}
                  >
                    <ArrowDown />
                  </Button>
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    aria-label="移除字段"
                    className="text-muted-foreground hover:text-destructive"
                    onClick={() => onChange(fields.filter((_, i) => i !== index))}
                  >
                    <X />
                  </Button>
                </div>
              </div>
              <div className="grid gap-2 sm:grid-cols-3">
                <Input
                  value={field.label}
                  onChange={(event) => update(index, { label: event.target.value })}
                  placeholder="名称，如：订单号"
                  className="h-8"
                />
                <Input
                  value={field.key}
                  onChange={(event) => update(index, { key: event.target.value })}
                  placeholder="标识，如：order_no"
                  className="h-8 font-mono"
                />
                <Select value={field.type} onValueChange={(value) => update(index, { type: value })}>
                  <SelectTrigger className="h-8 w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {FIELD_TYPES.map((type) => (
                      <SelectItem key={type.value} value={type.value}>
                        {type.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  value={field.placeholder ?? ""}
                  onChange={(event) => update(index, { placeholder: event.target.value })}
                  placeholder="输入提示（可选）"
                  className="h-8 min-w-40 flex-1"
                />
                <label className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Switch checked={field.required} onCheckedChange={(value) => update(index, { required: value })} />
                  必填
                </label>
              </div>
              {field.type === "select" ? (
                <Input
                  value={(field.options ?? []).join("，")}
                  onChange={(event) => update(index, { options: event.target.value.split(/[,，\n]/) })}
                  placeholder="选项，用逗号分隔：安卓，iOS，网页"
                  className="h-8"
                />
              ) : null}
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
