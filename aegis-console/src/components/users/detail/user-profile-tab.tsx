"use client";

import { useMemo, useState } from "react";
import { Cake, Layers, Loader2, Save, Settings2, UserRound } from "lucide-react";
import { toast } from "sonner";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger
} from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ApiError } from "@/lib/api-client";
import { useUpdateAdminAppUserProfileMutation } from "@/lib/admin-hooks";
import type { AdminAppUserDetail, AdminUserSettingsRecord } from "@/lib/api/types";
import {
  EMPTY,
  Fact,
  Facts,
  Panel,
  ValueTree,
  formatDate,
  formatTime,
  inputValue,
  numberText,
  textValue
} from "./user-detail-shared";

const CATEGORY_LABEL: Record<string, string> = {
  general: "通用",
  autoSign: "自动签到",
  notifications: "通知",
  privacy: "隐私",
  ui: "界面",
  security: "安全"
};

function collectCategories(user: AdminAppUserDetail) {
  const set = new Set<string>();
  for (const item of user.settings?.categories ?? []) set.add(item);
  for (const item of Object.keys(user.settings?.settings ?? {})) set.add(item);
  for (const item of user.settings?.configuredCategories ?? []) set.add(item);
  for (const item of user.settings?.missingCategories ?? []) set.add(item);
  return Array.from(set);
}

/**
 * 资料：可编辑的那一小部分 + 只读明细 + 用户端设置。
 *
 * 后端 `PUT /users/:userId/profile` 只接受 nickname 与 email —— 表单里就只放这两项。
 * 把只读字段做成输入框（哪怕 disabled）会让人以为改了能保存。
 */
export function UserProfileTab({
  appKey,
  userId,
  user
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
}) {
  const profile = user.profile;
  const mutation = useUpdateAdminAppUserProfileMutation(appKey, userId);
  const scope = `${appKey}:${userId}`;
  const categories = useMemo(() => collectCategories(user), [user]);

  // 草稿按 scope 绑定：换个用户时上一个人的未保存改动不会串过来，
  // 也不需要一个 useEffect 去同步（与配置面板同一约束）。
  const [draft, setDraft] = useState<{
    scope: string;
    value: { nickname: string; email: string };
  } | null>(null);

  const form =
    draft?.scope === scope
      ? draft.value
      : {
          nickname: inputValue(user.nickname ?? profile?.nickname),
          email: inputValue(user.email ?? profile?.email)
        };

  const dirty =
    form.nickname !== inputValue(user.nickname ?? profile?.nickname) ||
    form.email !== inputValue(user.email ?? profile?.email);

  const patch = (key: keyof typeof form, value: string) =>
    setDraft({ scope, value: { ...form, [key]: value } });

  async function handleSave() {
    try {
      await mutation.mutateAsync({
        nickname: form.nickname.trim() || undefined,
        email: form.email.trim() || undefined
      });
      setDraft(null);
      toast.success("资料已更新");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "更新失败");
    }
  }

  return (
    <div className="space-y-5">
      <div className="grid gap-5 xl:grid-cols-[0.85fr_1.15fr]">
        <Panel
          title="编辑资料"
          icon={<UserRound className="size-4" />}
          description="仅昵称与邮箱可由管理员直接修改，其余字段来自用户端或注册链路。"
          action={
            dirty ? (
              <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setDraft(null)}>
                重置
              </Button>
            ) : null
          }
        >
          <div className="space-y-4">
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">昵称</Label>
              <Input
                value={form.nickname}
                placeholder="未填写"
                onChange={(event) => patch("nickname", event.target.value)}
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">邮箱</Label>
              <Input
                value={form.email}
                placeholder="未填写"
                onChange={(event) => patch("email", event.target.value)}
              />
            </div>
            <Button size="sm" disabled={!dirty || mutation.isPending} onClick={handleSave}>
              {mutation.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Save className="size-3.5" />
              )}
              保存资料
            </Button>
          </div>
        </Panel>

        <Panel title="资料明细" icon={<Layers className="size-4" />}>
          <Facts columns={2}>
            <Fact label="头像地址" value={textValue(profile?.avatar || user.avatar)} mono />
            <Fact
              label="生日"
              icon={<Cake className="size-3" />}
              value={formatDate(profile?.birthday)}
            />
            <Fact label="角色" value={textValue(profile?.role)} />
            <Fact label="自定义 ID" value={textValue(profile?.customId)} mono />
            <Fact label="改 ID 次数" value={numberText(profile?.customIdCount)} />
            <Fact label="邀请码" value={textValue(profile?.inviteCode || user.inviteCode)} mono />
            <Fact label="上级邀请人" value={textValue(profile?.parentInviteAccount)} />
            <Fact label="标识码" value={textValue(profile?.markcode || user.markcode)} mono />
            <Fact label="资料更新时间" value={formatTime(profile?.updatedAt)} />
            <Fact label="账户更新时间" value={formatTime(user.updatedAt)} />
          </Facts>
        </Panel>
      </div>

      {Object.keys(profile?.extra ?? {}).length ? (
        <Panel
          title="附加字段"
          icon={<Layers className="size-4" />}
          description="user_profiles.extra 里的自由结构，由接入方写入。"
        >
          <ValueTree value={profile?.extra} />
        </Panel>
      ) : null}

      <Panel
        title="用户端设置"
        icon={<Settings2 className="size-4" />}
        description="用户在客户端里自己配的偏好。管理端只读——改这里等于替用户做决定。"
        action={
          <div className="flex items-center gap-1.5">
            <Badge variant="outline" size="sm">
              已配置 {user.settings?.configuredCategories?.length ?? 0}
            </Badge>
            {user.settings?.missingCategories?.length ? (
              <Badge variant="warning" size="sm">
                缺失 {user.settings.missingCategories.length}
              </Badge>
            ) : null}
          </div>
        }
        bodyClassName="p-0"
      >
        {categories.length ? (
          <Accordion type="multiple" className="w-full">
            {categories.map((category) => {
              const setting = user.settings?.settings?.[category] as
                | AdminUserSettingsRecord
                | undefined;
              const info = user.settings?.recordInfo?.[category];
              const configured = Boolean(setting);
              return (
                <AccordionItem key={category} value={category} className="px-5 last:border-b-0">
                  <AccordionTrigger className="py-3 text-sm hover:no-underline">
                    <div className="flex w-full items-center justify-between gap-4 pr-3">
                      <span className="font-medium">{CATEGORY_LABEL[category] ?? category}</span>
                      <div className="flex items-center gap-2">
                        {configured ? (
                          <span className="text-[11px] text-muted-foreground">
                            v{setting?.version ?? info?.version ?? 0} ·{" "}
                            {formatTime(setting?.updatedAt ?? info?.updatedAt)}
                          </span>
                        ) : null}
                        <Badge variant={configured ? "outline" : "warning"} size="sm">
                          {configured ? "已配置" : "未初始化"}
                        </Badge>
                      </div>
                    </div>
                  </AccordionTrigger>
                  <AccordionContent className="pb-4">
                    {setting?.settings && Object.keys(setting.settings).length ? (
                      <div className="rounded-xl border bg-muted/30 px-3 py-2.5">
                        <ValueTree value={setting.settings} />
                      </div>
                    ) : (
                      <div className="text-sm text-muted-foreground">
                        该分类没有落库记录，用户端读到的是默认值。
                      </div>
                    )}
                  </AccordionContent>
                </AccordionItem>
              );
            })}
          </Accordion>
        ) : (
          <div className="px-5 py-10 text-center text-sm text-muted-foreground">{EMPTY}</div>
        )}
      </Panel>
    </div>
  );
}
