"use client";

import { useMemo, useRef, useState } from "react";
import { Cake, ImageUp, Layers, Link2, Loader2, Save, Settings2, Trash2, UserRound } from "lucide-react";
import { toast } from "sonner";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger
} from "@/components/ui/accordion";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api-client";
import {
  useRemoveAdminAppUserAvatarMutation,
  useUpdateAdminAppUserProfileMutation,
  useUploadAdminAppUserAvatarMutation
} from "@/lib/admin-hooks";
import type { AdminAppUserDetail, AdminUserSettingsRecord } from "@/lib/api/types";
import {
  EMPTY,
  Fact,
  Facts,
  Panel,
  ValueTree,
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

/** 头像上传的入口限制：超过 8MB 的原图先在这里拦下，别等后端报错。 */
const AVATAR_MAX_BYTES = 8 * 1024 * 1024;

function collectCategories(user: AdminAppUserDetail) {
  const set = new Set<string>();
  for (const item of user.settings?.categories ?? []) set.add(item);
  for (const item of Object.keys(user.settings?.settings ?? {})) set.add(item);
  for (const item of user.settings?.configuredCategories ?? []) set.add(item);
  for (const item of user.settings?.missingCategories ?? []) set.add(item);
  return Array.from(set);
}

/** RFC3339 / 零值时间 → date input 的 YYYY-MM-DD；没有就空串。 */
function birthdayInputValue(raw?: string | null) {
  if (!raw) return "";
  const date = new Date(raw);
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return "";
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())}`;
}

type ProfileForm = {
  nickname: string;
  email: string;
  phone: string;
  birthday: string;
  bio: string;
};

/**
 * 资料：管理员可编辑的完整字段 + 头像管理 + 只读明细 + 用户端设置。
 *
 * 保存走按字段 PATCH：只发**改过的**字段。改过且为空 = 清空该字段
 * （后端语义：缺省不动、空串清空），生日清空单独走 clearBirthday。
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

  const original: ProfileForm = useMemo(
    () => ({
      nickname: inputValue(user.nickname ?? profile?.nickname),
      email: inputValue(user.email ?? profile?.email),
      phone: inputValue(user.phone ?? profile?.phone),
      birthday: birthdayInputValue(profile?.birthday),
      bio: inputValue(profile?.bio)
    }),
    [profile, user]
  );

  // 草稿按 scope 绑定：换个用户时上一个人的未保存改动不会串过来，
  // 也不需要一个 useEffect 去同步（与配置面板同一约束）。
  const [draft, setDraft] = useState<{ scope: string; value: ProfileForm } | null>(null);
  const form = draft?.scope === scope ? draft.value : original;

  const changedKeys = (Object.keys(original) as Array<keyof ProfileForm>).filter(
    (key) => form[key] !== original[key]
  );
  const dirty = changedKeys.length > 0;

  const patch = (key: keyof ProfileForm, value: string) =>
    setDraft({ scope, value: { ...form, [key]: value } });

  async function handleSave() {
    // 只发改过的字段：没动过的字段连键都不出现，后端一概不碰。
    const payload: Record<string, unknown> = {};
    for (const key of changedKeys) {
      if (key === "birthday") {
        const value = form.birthday.trim();
        if (value) payload.birthday = value;
        else payload.clearBirthday = true;
        continue;
      }
      payload[key] = form[key].trim();
    }
    try {
      await mutation.mutateAsync(payload);
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
          description="仅提交修改过的字段；清空后保存即清除该字段。"
          action={
            dirty ? (
              <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setDraft(null)}>
                重置
              </Button>
            ) : null
          }
        >
          <div className="space-y-4">
            <AvatarEditor appKey={appKey} userId={userId} user={user} />

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground">昵称</Label>
                <Input
                  value={form.nickname}
                  placeholder="未填写"
                  onChange={(event) => patch("nickname", event.target.value)}
                />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground">
                  生日
                  <Cake className="ml-1 inline size-3 align-[-2px]" />
                </Label>
                <Input
                  type="date"
                  value={form.birthday}
                  onChange={(event) => patch("birthday", event.target.value)}
                />
              </div>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">邮箱</Label>
              <Input
                value={form.email}
                placeholder="未填写"
                onChange={(event) => patch("email", event.target.value)}
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">手机号</Label>
              <Input
                value={form.phone}
                placeholder="未填写"
                onChange={(event) => patch("phone", event.target.value)}
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">个人简介</Label>
              <Textarea
                value={form.bio}
                rows={3}
                placeholder="未填写"
                onChange={(event) => patch("bio", event.target.value)}
              />
            </div>
            <Button size="sm" disabled={!dirty || mutation.isPending} onClick={handleSave}>
              {mutation.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Save className="size-3.5" />
              )}
              保存资料
              {dirty ? <span className="text-[10px] opacity-70">（{changedKeys.length} 项改动）</span> : null}
            </Button>
          </div>
        </Panel>

        <Panel title="资料明细" icon={<Layers className="size-4" />}>
          <Facts columns={2}>
            <Fact label="角色" value={textValue(profile?.role)} />
            <Fact label="自定义 ID" value={textValue(profile?.customId)} mono />
            <Fact label="改 ID 次数" value={numberText(profile?.customIdCount)} />
            <Fact label="邀请码" value={textValue(profile?.inviteCode || user.inviteCode)} mono />
            <Fact label="上级邀请人" value={textValue(profile?.parentInviteAccount)} />
            <Fact label="标识码" value={textValue(profile?.markcode || user.markcode)} mono />
            <Fact label="注册 IP" value={textValue(profile?.registerIp || user.registerIP)} mono />
            <Fact label="注册地" value={textValue([profile?.registerProvince, profile?.registerCity].filter(Boolean).join(" "))} />
            <Fact label="资料更新时间" value={formatTime(profile?.updatedAt)} />
            <Fact label="账户更新时间" value={formatTime(user.updatedAt)} />
          </Facts>
        </Panel>
      </div>

      {Object.keys(profile?.extra ?? {}).length ? (
        <Panel
          title="附加字段"
          icon={<Layers className="size-4" />}
          description="由接入方写入的扩展数据。"
        >
          <ValueTree value={profile?.extra} />
        </Panel>
      ) : null}

      <Panel
        title="用户端设置"
        icon={<Settings2 className="size-4" />}
        description="用户客户端偏好，管理端只读。"
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

/**
 * 头像编辑：上传文件 / 填外链 / 移除。
 *
 * 三个动作即时生效，不进"保存资料"的草稿 —— 上传本身就是一次落库，
 * 让它再等一次保存按钮只会造出"预览换了、刷新又回去了"的错觉。
 * 外链走资料 PATCH 的 avatar 字段（后端只收 http(s)，storage:// 会被拒绝）。
 */
function AvatarEditor({
  appKey,
  userId,
  user
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
}) {
  const fileRef = useRef<HTMLInputElement>(null);
  const uploadMutation = useUploadAdminAppUserAvatarMutation(appKey, userId);
  const removeMutation = useRemoveAdminAppUserAvatarMutation(appKey, userId);
  const profileMutation = useUpdateAdminAppUserProfileMutation(appKey, userId);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [linkEditing, setLinkEditing] = useState(false);
  const [link, setLink] = useState("");

  const avatar = user.avatar || user.profile?.avatar || "";
  const fallback = String(user.nickname || user.account || "U").trim().slice(0, 2).toUpperCase();
  const busy = uploadMutation.isPending || removeMutation.isPending || profileMutation.isPending;

  async function handleFile(file: File | undefined) {
    if (!file) return;
    if (!file.type.startsWith("image/")) {
      toast.error("只支持图片文件");
      return;
    }
    if (file.size > AVATAR_MAX_BYTES) {
      toast.error("图片不能超过 8MB");
      return;
    }
    try {
      await uploadMutation.mutateAsync(file);
      toast.success("头像已更新");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "上传失败");
    } finally {
      if (fileRef.current) fileRef.current.value = "";
    }
  }

  async function handleLinkSave() {
    const value = link.trim();
    if (!value) return;
    try {
      await profileMutation.mutateAsync({ avatar: value });
      toast.success("头像外链已设置");
      setLinkEditing(false);
      setLink("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "设置失败");
    }
  }

  return (
    <div className="space-y-2.5 rounded-xl border bg-muted/20 p-3">
      <div className="flex items-center gap-3">
        <Avatar className="size-14 rounded-xl border">
          <AvatarImage src={avatar} />
          <AvatarFallback className="rounded-xl text-sm">{fallback}</AvatarFallback>
        </Avatar>
        <div className="min-w-0 flex-1 space-y-1">
          <div className="text-xs font-medium">头像</div>
          <p className="text-[11px] leading-4 text-muted-foreground">
            上传即时生效；移除后恢复默认头像。
          </p>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        <input
          ref={fileRef}
          type="file"
          accept="image/*"
          className="hidden"
          aria-label="选择头像图片"
          onChange={(event) => handleFile(event.target.files?.[0])}
        />
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-xs"
          disabled={busy}
          onClick={() => fileRef.current?.click()}
        >
          {uploadMutation.isPending ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <ImageUp className="size-3.5" />
          )}
          上传图片
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-xs"
          disabled={busy}
          onClick={() => setLinkEditing((prev) => !prev)}
        >
          <Link2 className="size-3.5" />
          外链地址
        </Button>
        <Button
          size="sm"
          variant="ghost"
          className="h-7 text-xs text-destructive hover:text-destructive"
          disabled={busy}
          onClick={() => setConfirmRemove(true)}
        >
          {removeMutation.isPending ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <Trash2 className="size-3.5" />
          )}
          移除头像
        </Button>
      </div>
      {linkEditing ? (
        <div className="flex items-center gap-1.5">
          <Input
            value={link}
            placeholder="https://example.com/avatar.png"
            className="h-7 flex-1 text-xs"
            onChange={(event) => setLink(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") void handleLinkSave();
            }}
          />
          <Button
            size="sm"
            className="h-7 text-xs"
            disabled={!link.trim() || profileMutation.isPending}
            onClick={handleLinkSave}
          >
            {profileMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : "设置"}
          </Button>
        </div>
      ) : null}

      <AlertDialog open={confirmRemove} onOpenChange={setConfirmRemove}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>移除头像</AlertDialogTitle>
            <AlertDialogDescription>
              移除后恢复为默认头像，即时生效。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                try {
                  await removeMutation.mutateAsync();
                  toast.success("已移除头像");
                } catch (error) {
                  toast.error(error instanceof ApiError ? error.message : "移除失败");
                }
              }}
            >
              移除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
