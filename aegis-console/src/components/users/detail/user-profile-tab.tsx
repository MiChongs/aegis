"use client";

import { useEffect, useMemo, useRef, useState, type ComponentType, type DragEvent, type ReactNode } from "react";
import {
  AtSign,
  Braces,
  Cake,
  Check,
  ClipboardCopy,
  Contact,
  FileText,
  Fingerprint,
  Hash,
  ImageUp,
  Link2,
  List,
  Loader2,
  Mail,
  MapPin,
  Network,
  Pencil,
  Phone,
  Shapes,
  ShieldUser,
  Smartphone,
  Ticket,
  Trash2,
  Undo2,
  UserPlus,
  UserRound,
  CalendarClock
} from "lucide-react";
import { toast } from "sonner";
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
import { BrandIcon, getBrandIcon } from "@/components/ui/brand-icon";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { JsonViewer } from "@/components/ui/json-viewer";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useNow } from "@/components/users/ban-form";
import { ApiError } from "@/lib/api-client";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import {
  useRemoveAdminAppUserAvatarMutation,
  useUpdateAdminAppUserProfileMutation,
  useUploadAdminAppUserAvatarMutation
} from "@/lib/admin-hooks";
import type { AdminAppUserDetail } from "@/lib/api/types";
import { formatDateTime, formatRelative, localDateKey } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { CopyButton, InfoGrid, SearchField, Section, StateBlock, ToneIcon, copyToClipboard, type InfoItem } from "./kit";
import { UserRenameDialog } from "./user-account-setup";
import { inputValue } from "./user-detail-shared";
import { UserPreferences, humanizeKey, readableValue, updatedText } from "./user-preferences";

/**
 * 资料页签：这个人是谁、怎么联系、从哪来、喜欢怎么用。
 *
 *   基本资料   头像（上传 / 图片地址 / 移除，即时生效）+ 只读字段网格；
 *              右上「编辑」进入编辑态：分组表单 + 底部固定的保存条（显示未保存改动数）
 *   联系方式   邮箱、手机号（可编辑）与用户在客户端维护的第三方账号
 *   个人简介   全文，可复制、可编辑
 *   注册来源   注册时间、IP、归属地、运营商、设备标识码、邀请码、上级邀请人（只读，可复制）
 *   附加字段   应用写进 extra 的数据，键值卡片或 JSON 两种看法
 *   偏好设置   用户端设置翻译成人话（见 user-preferences）
 *
 * 保存走按字段 PATCH：只发**改过的**字段；改过且为空 = 清空（生日走 clearBirthday）。
 * 管理端可写的只有昵称、邮箱、手机号、生日、简介、头像（后端 AdminUserProfilePatch），
 * 自定义 ID、第三方联系方式、附加字段与偏好设置没有管理端写接口，这里只读。
 * 账号（用户名）走单独的改名接口（user-account-setup 的 UserRenameDialog），不进这张表单。
 */

/** 头像上传的入口限制：超过 8MB 的原图先在这里拦下，别等后端报错。 */
const AVATAR_MAX_BYTES = 8 * 1024 * 1024;
/** 与后端 user_service.go 的 profile*MaxLen 一致 */
const NICKNAME_MAX = 128;
const EMAIL_MAX = 255;
const PHONE_MAX = 32;
const AVATAR_URL_MAX = 1024;
const BIO_MAX = 2000;
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const PHONE_RE = /^\+?[0-9\s\-()]+$/;

const CONTACT_META: Record<string, { label: string; icon?: ComponentType<{ className?: string }> }> = {
  wechat: { label: "微信" },
  qq: { label: "QQ" },
  telegram: { label: "Telegram" },
  discord: { label: "Discord" },
  twitter: { label: "X" },
  github: { label: "GitHub" },
  gitee: { label: "Gitee" },
  weibo: { label: "微博" },
  phone: { label: "电话", icon: Phone },
  email: { label: "邮箱", icon: Mail },
  other: { label: "其他", icon: AtSign }
};

/** RFC3339 / 零值时间 → date input 的 YYYY-MM-DD；没有就空串。生日是日历日，按 UTC 取年月日。 */
function birthdayInputValue(raw?: string | null) {
  if (!raw) return "";
  const date = new Date(raw);
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return "";
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())}`;
}

/** 「1998 年 3 月 14 日」「28 岁」「还有 12 天过生日」 */
function describeBirthday(value: string, now: number) {
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})$/);
  if (!match) return null;
  const [year, month, day] = [Number(match[1]), Number(match[2]), Number(match[3])];
  const today = new Date(now);
  let age = today.getFullYear() - year;
  const thisYear = new Date(today.getFullYear(), month - 1, day);
  const todayStart = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  if (todayStart < thisYear) age -= 1;
  let next = thisYear;
  if (next < todayStart) next = new Date(today.getFullYear() + 1, month - 1, day);
  const days = Math.round((next.getTime() - todayStart.getTime()) / 86_400_000);
  return {
    date: `${year} 年 ${month} 月 ${day} 日`,
    age: age >= 0 && age < 150 ? `${age} 岁` : "",
    countdown: days === 0 ? "今天生日" : days <= 30 ? `还有 ${days} 天过生日` : ""
  };
}

function charCount(value: string) {
  return Array.from(value.trim()).length;
}

function str(value: unknown) {
  return typeof value === "string" ? value.trim() : typeof value === "number" ? String(value) : "";
}

type ProfileForm = {
  nickname: string;
  birthday: string;
  email: string;
  phone: string;
  bio: string;
};

type FieldKey = keyof ProfileForm;

const FIELD_ORDER: FieldKey[] = ["nickname", "birthday", "email", "phone", "bio"];

const FIELD_LABEL: Record<FieldKey, string> = {
  nickname: "昵称",
  birthday: "生日",
  email: "邮箱",
  phone: "手机号",
  bio: "简介"
};

/** 规则与后端 validateAdminUserProfilePatch 相同（手机号格式是前端多拦的一道），当场提示，省一次往返。 */
function validate(form: ProfileForm, today: string) {
  const errors: Partial<Record<FieldKey, string>> = {};
  if (charCount(form.nickname) > NICKNAME_MAX) errors.nickname = `昵称最多 ${NICKNAME_MAX} 个字符`;

  const email = form.email.trim();
  if (charCount(email) > EMAIL_MAX) errors.email = `邮箱最多 ${EMAIL_MAX} 个字符`;
  else if (email && !EMAIL_RE.test(email)) errors.email = "邮箱格式不正确";

  const phone = form.phone.trim();
  if (charCount(phone) > PHONE_MAX) errors.phone = `手机号最多 ${PHONE_MAX} 个字符`;
  else if (phone && !PHONE_RE.test(phone)) errors.phone = "手机号只能包含数字、空格、+、- 和括号";
  else if (phone) {
    const digits = phone.replace(/\D/g, "").length;
    if (digits < 5 || digits > 20) errors.phone = "手机号位数不正确";
  }

  if (charCount(form.bio) > BIO_MAX) errors.bio = `简介最多 ${BIO_MAX} 个字符`;

  if (form.birthday) {
    if (form.birthday > today) errors.birthday = "生日不能是未来的日期";
    else if (form.birthday < "1900-01-01") errors.birthday = "生日不能早于 1900 年";
  }
  return errors;
}

function apiError(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback;
}

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
  const now = useNow(60 * 60_000);
  const today = localDateKey(new Date(now));

  const original: ProfileForm = useMemo(
    () => ({
      nickname: inputValue(user.nickname ?? profile?.nickname),
      birthday: birthdayInputValue(profile?.birthday),
      email: inputValue(user.email ?? profile?.email),
      phone: inputValue(user.phone ?? profile?.phone),
      bio: inputValue(profile?.bio)
    }),
    [profile, user]
  );

  // 编辑态与草稿都按 scope 绑定：换个用户时上一个人的未保存改动不会串过来，
  // 也不需要一个 useEffect 去同步。
  const [editScope, setEditScope] = useState<string | null>(null);
  const [draft, setDraft] = useState<{ scope: string; value: ProfileForm } | null>(null);
  const [confirmDiscard, setConfirmDiscard] = useState(false);
  const [focusRequest, setFocusRequest] = useState<{ field: FieldKey; seq: number } | null>(null);
  const editing = editScope === scope;
  const form = draft?.scope === scope ? draft.value : original;

  const changedKeys = FIELD_ORDER.filter((key) => form[key] !== original[key]);
  const dirty = changedKeys.length > 0;
  const errors = validate(form, today);
  const errorCount = Object.keys(errors).length;
  const invalid = errorCount > 0;

  // 从「联系方式」「个人简介」点编辑时，把光标送到对应输入框
  useEffect(() => {
    if (!editing || !focusRequest) return;
    const el = document.getElementById(`profile-field-${focusRequest.field}`);
    if (el) {
      el.scrollIntoView({ block: "center", behavior: "smooth" });
      (el as HTMLInputElement).focus({ preventScroll: true });
    }
  }, [editing, focusRequest]);

  // 有未保存改动时离开页面先提醒
  useEffect(() => {
    if (!editing || !dirty) return;
    const handler = (event: BeforeUnloadEvent) => {
      event.preventDefault();
    };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [editing, dirty]);

  const patch = (key: FieldKey, value: string) => setDraft({ scope, value: { ...form, [key]: value } });
  const revert = (key: FieldKey) => patch(key, original[key]);

  function startEdit(field?: FieldKey) {
    setEditScope(scope);
    if (field) setFocusRequest((prev) => ({ field, seq: (prev?.seq ?? 0) + 1 }));
  }

  function finishEdit() {
    setDraft(null);
    setEditScope(null);
    setConfirmDiscard(false);
  }

  function requestCancel() {
    if (dirty) setConfirmDiscard(true);
    else finishEdit();
  }

  async function handleSave() {
    if (!dirty || invalid || mutation.isPending) return;
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
      finishEdit();
      toast.success(`资料已更新（${changedKeys.map((k) => FIELD_LABEL[k]).join("、")}）`);
    } catch (error) {
      toast.error(apiError(error, "更新失败"));
    }
  }

  return (
    <div className="flex flex-col gap-5">
      <BasicSection
        appKey={appKey}
        userId={userId}
        user={user}
        now={now}
        editing={editing}
        onEdit={() => startEdit("nickname")}
      >
        {editing ? (
          <ProfileEditForm
            form={form}
            original={original}
            errors={errors}
            today={today}
            disabled={mutation.isPending}
            onChange={patch}
            onRevert={revert}
            onSubmit={handleSave}
          />
        ) : null}
      </BasicSection>

      {editing ? (
        <SaveBar
          changedKeys={changedKeys}
          errorCount={errorCount}
          pending={mutation.isPending}
          onRevertAll={() => setDraft(null)}
          onCancel={requestCancel}
        />
      ) : null}

      <div className="grid items-start gap-5 xl:grid-cols-2">
        <ContactsSection user={user} onEdit={() => startEdit("email")} />
        <BioSection bio={(profile?.bio ?? "").trim()} onEdit={() => startEdit("bio")} />
      </div>

      <RegistrationSection user={user} />

      <ExtraFieldsSection user={user} />

      <UserPreferences user={user} />

      <AlertDialog open={confirmDiscard} onOpenChange={setConfirmDiscard}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃未保存的改动</AlertDialogTitle>
            <AlertDialogDescription>
              {changedKeys.map((k) => FIELD_LABEL[k]).join("、")} 的修改尚未保存，退出编辑后将丢失。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>继续编辑</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={finishEdit}>
              放弃改动
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// ── 基本资料 ──────────────────────────

function BasicSection({
  appKey,
  userId,
  user,
  now,
  editing,
  onEdit,
  children
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
  now: number;
  editing: boolean;
  onEdit: () => void;
  children?: ReactNode;
}) {
  const profile = user.profile;
  const can = useAppKeyPermissionChecker(appKey);
  const canRename = can("app:user:edit");
  const [renaming, setRenaming] = useState(false);
  const nickname = (user.nickname || profile?.nickname || "").trim();
  const birthday = describeBirthday(birthdayInputValue(profile?.birthday), now);
  const publicId = user.publicId ? String(user.publicId) : "";
  const customId = (profile?.customId ?? "").trim();
  const customIdCount = typeof profile?.customIdCount === "number" ? profile.customIdCount : null;

  const items: InfoItem[] = [
    { label: "昵称", icon: UserRound, value: nickname, copy: nickname || null },
    {
      label: "账号",
      icon: AtSign,
      value: user.account,
      mono: true,
      copy: user.account || null,
      hint: canRename ? (
        <button type="button" className="underline-offset-2 hover:text-foreground hover:underline" onClick={() => setRenaming(true)}>
          修改用户名
        </button>
      ) : undefined
    },
    { label: "用户 ID", icon: Hash, value: publicId, mono: true, copy: publicId || null, hint: publicId ? "用户在客户端看到的编号" : undefined },
    {
      label: "自定义 ID",
      icon: Fingerprint,
      value: customId,
      mono: true,
      copy: customId || null,
      hint: customIdCount !== null ? `已修改 ${customIdCount} 次，由用户在客户端设置` : undefined
    },
    {
      label: "角色",
      icon: ShieldUser,
      value: profile?.role ? (
        <Badge variant="secondary" size="sm">
          {profile.role}
        </Badge>
      ) : undefined
    },
    {
      label: "生日",
      icon: Cake,
      value: birthday ? [birthday.date, birthday.age].filter(Boolean).join("，") : undefined,
      hint: birthday?.countdown ? (
        <Badge variant="info" size="sm">
          {birthday.countdown}
        </Badge>
      ) : undefined
    },
    { label: "内部编号", icon: Hash, value: String(userId), mono: true, copy: String(userId) },
    {
      label: "资料更新",
      icon: CalendarClock,
      value: profile?.updatedAt ? formatDateTime(profile.updatedAt) : undefined,
      hint: profile?.updatedAt ? formatRelative(profile.updatedAt) : undefined
    }
  ];

  return (
    <Section
      title="基本资料"
      icon={UserRound}
      tone="primary"
      description={
        editing
          ? "只提交改过的字段，清空后保存即删除该项。头像修改即时生效。"
          : profile?.updatedAt
            ? `资料${updatedText(profile.updatedAt)}`
            : "称呼、身份标识与生日"
      }
      actions={
        editing ? (
          <Badge variant="warning" size="sm">
            编辑中
          </Badge>
        ) : (
          <Button size="sm" variant="outline" onClick={onEdit}>
            <Pencil />
            编辑
          </Button>
        )
      }
    >
      <div className="flex flex-col gap-6 md:flex-row md:items-start">
        <AvatarPanel appKey={appKey} userId={userId} user={user} />
        <div className="min-w-0 flex-1">{editing ? children : <InfoGrid items={items} columns={2} />}</div>
      </div>
      {renaming ? (
        <UserRenameDialog appKey={appKey} userId={userId} currentAccount={user.account ?? ""} onClose={() => setRenaming(false)} />
      ) : null}
    </Section>
  );
}

// ── 编辑表单 ──────────────────────────

function ProfileEditForm({
  form,
  original,
  errors,
  today,
  disabled,
  onChange,
  onRevert,
  onSubmit
}: {
  form: ProfileForm;
  original: ProfileForm;
  errors: Partial<Record<FieldKey, string>>;
  today: string;
  disabled: boolean;
  onChange: (key: FieldKey, value: string) => void;
  onRevert: (key: FieldKey) => void;
  onSubmit: () => void;
}) {
  const field = (key: FieldKey) => ({
    id: `profile-field-${key}`,
    label: FIELD_LABEL[key],
    changed: form[key] !== original[key],
    error: errors[key],
    onRevert: () => onRevert(key)
  });
  const input = (key: FieldKey) => ({
    id: `profile-field-${key}`,
    value: form[key],
    disabled,
    "aria-invalid": errors[key] ? true : undefined,
    "aria-describedby": errors[key] ? `profile-field-${key}-error` : undefined,
    onChange: (event: { target: { value: string } }) => onChange(key, event.target.value)
  });
  const bioCount = charCount(form.bio);

  return (
    <form
      id="profile-edit-form"
      className="flex flex-col gap-6"
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
    >
      <FieldGroup title="称呼与生日">
        <Field {...field("nickname")} hint={<Counter value={charCount(form.nickname)} max={NICKNAME_MAX} />}>
          <Input {...input("nickname")} placeholder="未填写" autoComplete="off" />
        </Field>
        <Field {...field("birthday")}>
          <Input {...input("birthday")} type="date" min="1900-01-01" max={today} />
        </Field>
      </FieldGroup>

      <FieldGroup title="联系方式">
        <Field {...field("email")}>
          <Input {...input("email")} type="email" placeholder="未填写" autoComplete="off" />
        </Field>
        <Field {...field("phone")}>
          <Input {...input("phone")} inputMode="tel" placeholder="未填写，可带国家区号如 +86" autoComplete="off" />
        </Field>
      </FieldGroup>

      <FieldGroup title="个人简介" single>
        <Field {...field("bio")} label="简介" hint={<Counter value={bioCount} max={BIO_MAX} />}>
          <Textarea {...input("bio")} rows={5} placeholder="未填写" className="resize-y" />
        </Field>
      </FieldGroup>

      <p className="text-xs leading-5 text-muted-foreground">
        自定义 ID、第三方联系方式、附加字段与偏好设置由用户在客户端维护，管理端只读；账号在「修改用户名」中修改。
      </p>
      {/* 让回车提交表单；可见的保存按钮在底部固定条里 */}
      <button type="submit" className="hidden" aria-hidden tabIndex={-1} />
    </form>
  );
}

function Counter({ value, max }: { value: number; max: number }) {
  return <span className={cn("tabular-nums", value > max && "text-destructive")}>{value} / {max}</span>;
}

function FieldGroup({ title, single, children }: { title: string; single?: boolean; children: ReactNode }) {
  return (
    <fieldset className="flex min-w-0 flex-col gap-3">
      <legend className="mb-3 text-xs font-medium text-muted-foreground">{title}</legend>
      <div className={cn("grid gap-4", !single && "sm:grid-cols-2")}>{children}</div>
    </fieldset>
  );
}

function Field({
  id,
  label,
  changed,
  error,
  hint,
  onRevert,
  children
}: {
  id: string;
  label: string;
  changed: boolean;
  error?: string;
  hint?: ReactNode;
  onRevert: () => void;
  children: ReactNode;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <div className="flex min-h-5 items-center justify-between gap-2">
        <Label htmlFor={id} className="gap-1.5 text-xs">
          {label}
          {changed ? <span className="size-1.5 rounded-full bg-amber-500" aria-label="已修改" /> : null}
        </Label>
        <span className="flex items-center gap-2 text-xs text-muted-foreground">
          {hint}
          {changed ? (
            <button type="button" className="inline-flex items-center gap-0.5 transition-colors hover:text-foreground" onClick={onRevert}>
              <Undo2 className="size-3" />
              还原
            </button>
          ) : null}
        </span>
      </div>
      {children}
      {error ? (
        <p id={`${id}-error`} className="text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  );
}

function SaveBar({
  changedKeys,
  errorCount,
  pending,
  onRevertAll,
  onCancel
}: {
  changedKeys: FieldKey[];
  errorCount: number;
  pending: boolean;
  onRevertAll: () => void;
  onCancel: () => void;
}) {
  const dirty = changedKeys.length > 0;
  return (
    <div className="sticky bottom-3 z-20">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border bg-background/95 px-4 py-2.5 shadow-lg backdrop-blur supports-[backdrop-filter]:bg-background/80">
        <div className="flex min-w-0 flex-1 items-center gap-2 text-xs">
          {dirty ? (
            <>
              <Badge variant="warning" size="sm" className="tabular-nums">
                {changedKeys.length} 项未保存
              </Badge>
              <span className="min-w-0 truncate text-muted-foreground">{changedKeys.map((k) => FIELD_LABEL[k]).join("、")}</span>
            </>
          ) : (
            <span className="text-muted-foreground">尚未修改任何字段</span>
          )}
          {errorCount ? (
            <Badge variant="danger" size="sm" className="tabular-nums">
              {errorCount} 项需修正
            </Badge>
          ) : null}
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          <Button size="sm" variant="ghost" disabled={pending} onClick={onCancel}>
            取消
          </Button>
          <Button size="sm" variant="outline" disabled={!dirty || pending} onClick={onRevertAll}>
            <Undo2 />
            全部还原
          </Button>
          <Button size="sm" type="submit" form="profile-edit-form" disabled={!dirty || errorCount > 0 || pending}>
            {pending ? <Loader2 className="animate-spin" /> : <Check />}
            保存
          </Button>
        </div>
      </div>
    </div>
  );
}

// ── 头像 ──────────────────────────

/**
 * 头像：上传文件（按钮或拖入）/ 填外链 / 移除，点头像看大图。
 *
 * 三个动作即时生效，不进「保存」的草稿 —— 上传本身就是一次落库，
 * 让它再等一次保存按钮只会造出「预览换了、刷新又回去了」的错觉。
 * 外链走资料 PATCH 的 avatar 字段（后端只收 http(s)，storage:// 会被拒绝）。
 */
function AvatarPanel({ appKey, userId, user }: { appKey: string; userId: number; user: AdminAppUserDetail }) {
  const fileRef = useRef<HTMLInputElement>(null);
  const uploadMutation = useUploadAdminAppUserAvatarMutation(appKey, userId);
  const removeMutation = useRemoveAdminAppUserAvatarMutation(appKey, userId);
  const profileMutation = useUpdateAdminAppUserProfileMutation(appKey, userId);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [linkOpen, setLinkOpen] = useState(false);
  const [link, setLink] = useState("");
  const [dragging, setDragging] = useState(false);

  const avatar = (user.avatar || user.profile?.avatar || "").trim();
  const fallback = String(user.nickname || user.account || "U").trim().slice(0, 2).toUpperCase();
  const busy = uploadMutation.isPending || removeMutation.isPending || profileMutation.isPending;

  const linkValue = link.trim();
  const linkError = !linkValue
    ? ""
    : !/^https?:\/\/\S+$/i.test(linkValue)
      ? "请填写以 http:// 或 https:// 开头的图片地址"
      : linkValue.length > AVATAR_URL_MAX
        ? `地址最多 ${AVATAR_URL_MAX} 个字符`
        : "";

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
      toast.error(apiError(error, "上传失败"));
    } finally {
      if (fileRef.current) fileRef.current.value = "";
    }
  }

  async function handleLinkSave() {
    if (!linkValue || linkError) return;
    try {
      await profileMutation.mutateAsync({ avatar: linkValue });
      toast.success("头像已更新");
      setLinkOpen(false);
      setLink("");
    } catch (error) {
      toast.error(apiError(error, "设置失败"));
    }
  }

  async function handleRemove() {
    try {
      await removeMutation.mutateAsync();
      toast.success("已移除头像");
      setConfirmRemove(false);
    } catch (error) {
      toast.error(apiError(error, "移除失败"));
    }
  }

  const dragProps = {
    onDragOver: (event: DragEvent) => {
      if (busy || !event.dataTransfer.types.includes("Files")) return;
      event.preventDefault();
      setDragging(true);
    },
    onDragLeave: () => setDragging(false),
    onDrop: (event: DragEvent) => {
      event.preventDefault();
      setDragging(false);
      if (!busy) void handleFile(event.dataTransfer.files?.[0]);
    }
  };

  return (
    <div className="flex shrink-0 flex-col items-center gap-3 md:w-44">
      <div className="relative" {...dragProps}>
        <Avatar className="size-28 rounded-2xl border">
          <AvatarImage src={avatar || undefined} alt="用户头像" className="object-cover" />
          <AvatarFallback className="rounded-2xl text-2xl font-medium">{fallback}</AvatarFallback>
        </Avatar>
        {busy || dragging ? (
          <span
            className={cn(
              "pointer-events-none absolute inset-0 grid place-items-center rounded-2xl",
              dragging ? "border-2 border-dashed border-primary bg-primary/10" : "bg-background/60"
            )}
          >
            {dragging ? <ImageUp className="size-6 text-primary" /> : <Loader2 className="size-6 animate-spin text-muted-foreground" />}
          </span>
        ) : null}
      </div>

      <div className="flex flex-wrap justify-center gap-2">
        <Button size="sm" variant="outline" disabled={busy} onClick={() => fileRef.current?.click()}>
          <ImageUp />
          上传
        </Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => setLinkOpen(true)}>
          <Link2 />
          图片地址
        </Button>
        {avatar ? (
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirmRemove(true)}>
            <Trash2 />
            移除
          </Button>
        ) : null}
      </div>
      <div className="flex flex-col items-center gap-1 text-center text-xs leading-5 text-muted-foreground">
        <span>点击头像查看大图，可直接拖入图片</span>
        <span>图片最大 8MB，修改即时生效</span>
        {avatar ? (
          <button
            type="button"
            className="inline-flex items-center gap-1 transition-colors hover:text-foreground"
            onClick={() => void copyToClipboard(avatar, "头像地址")}
          >
            <ClipboardCopy className="size-3" />
            复制头像地址
          </button>
        ) : null}
      </div>

      <input
        ref={fileRef}
        type="file"
        accept="image/*"
        className="hidden"
        aria-label="选择头像图片"
        onChange={(event) => handleFile(event.target.files?.[0])}
      />

      <Dialog
        open={linkOpen}
        onOpenChange={(open) => {
          setLinkOpen(open);
          if (!open) setLink("");
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>使用图片地址</DialogTitle>
            <DialogDescription>填写外部图片的 http(s) 地址作为头像，保存后立即生效。</DialogDescription>
          </DialogHeader>
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              void handleLinkSave();
            }}
          >
            <div className="flex items-center gap-3">
              <Avatar className="size-14 rounded-xl border" preview={false}>
                <AvatarImage src={linkValue && !linkError ? linkValue : undefined} alt="预览" className="object-cover" />
                <AvatarFallback className="rounded-xl text-xs text-muted-foreground">预览</AvatarFallback>
              </Avatar>
              <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                <Label htmlFor="avatar-link" className="text-xs">
                  图片地址
                </Label>
                <Input
                  id="avatar-link"
                  autoFocus
                  value={link}
                  placeholder="https://"
                  aria-invalid={linkError ? true : undefined}
                  onChange={(event) => setLink(event.target.value)}
                />
              </div>
            </div>
            {linkError ? <p className="text-xs text-destructive">{linkError}</p> : null}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setLinkOpen(false)}>
                取消
              </Button>
              <Button type="submit" disabled={!linkValue || Boolean(linkError) || profileMutation.isPending}>
                {profileMutation.isPending ? <Loader2 className="animate-spin" /> : <Check />}
                设为头像
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={confirmRemove} onOpenChange={setConfirmRemove}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>移除头像</AlertDialogTitle>
            <AlertDialogDescription>移除后恢复为默认头像，用户可在客户端重新上传。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={removeMutation.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={removeMutation.isPending}
              onClick={(event) => {
                event.preventDefault();
                void handleRemove();
              }}
            >
              {removeMutation.isPending ? <Loader2 className="animate-spin" /> : null}
              移除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// ── 联系方式 ──────────────────────────

type ContactRowData = {
  key: string;
  icon: ComponentType<{ className?: string }>;
  brand?: string;
  label: string;
  value: string;
  href?: string;
  mono?: boolean;
  tag?: string;
};

function ContactsSection({ user, onEdit }: { user: AdminAppUserDetail; onEdit: () => void }) {
  const profile = user.profile;
  const email = (user.email || profile?.email || "").trim();
  const phone = (user.phone || profile?.phone || "").trim();
  const contacts = (profile?.contacts ?? []).filter((c) => c.value?.trim());

  const rows: ContactRowData[] = [
    { key: "email", icon: Mail, label: "邮箱", value: email, href: email ? `mailto:${email}` : undefined },
    { key: "phone", icon: Smartphone, label: "手机号", value: phone, href: phone ? `tel:${phone.replace(/[\s()-]/g, "")}` : undefined, mono: true },
    ...contacts.map((contact, i): ContactRowData => {
      const key = contact.platform?.toLowerCase() || "other";
      const meta = CONTACT_META[key] ?? { label: humanizeKey(key) };
      return {
        key: `${key}-${i}`,
        icon: meta.icon ?? AtSign,
        brand: getBrandIcon(key) ? key : undefined,
        label: contact.label || meta.label,
        value: contact.value.trim(),
        tag: "客户端"
      };
    })
  ];

  return (
    <Section
      title="联系方式"
      icon={Contact}
      tone="info"
      description={
        contacts.length
          ? `邮箱与手机号可编辑，另有 ${contacts.length} 个由用户维护的第三方账号`
          : "邮箱与手机号可编辑，第三方账号由用户在客户端维护"
      }
      actions={
        <Button size="sm" variant="ghost" onClick={onEdit}>
          <Pencil />
          编辑
        </Button>
      }
      flush
    >
      <ul className="divide-y">
        {rows.map((row) => (
          <ContactRow key={row.key} row={row} />
        ))}
      </ul>
    </Section>
  );
}

function ContactRow({ row }: { row: ContactRowData }) {
  const text = row.value;
  const content = (
    <span className={cn("min-w-0 truncate text-sm", row.mono && "font-mono text-[13px] tabular-nums", !text && "text-muted-foreground")}>
      {text || "未填写"}
    </span>
  );
  return (
    <li className="flex min-w-0 items-center gap-3 px-4 py-2.5 sm:px-5">
      {row.brand ? (
        <span className="inline-flex size-7 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
          <BrandIcon slug={row.brand} className="size-3.5" />
        </span>
      ) : (
        <ToneIcon icon={row.icon} tone={text ? "info" : "neutral"} size="sm" />
      )}
      <span className="w-14 shrink-0 truncate text-xs text-muted-foreground" title={row.label}>
        {row.label}
      </span>
      {text && row.href ? (
        <a href={row.href} className="flex min-w-0 underline-offset-2 hover:underline">
          {content}
        </a>
      ) : (
        content
      )}
      <span className="ml-auto flex shrink-0 items-center gap-1.5">
        {row.tag ? (
          <Badge variant="outline" size="sm">
            {row.tag}
          </Badge>
        ) : null}
        {text ? <CopyButton value={text} label={row.label} /> : null}
      </span>
    </li>
  );
}

// ── 个人简介 ──────────────────────────

function BioSection({ bio, onEdit }: { bio: string; onEdit: () => void }) {
  const count = Array.from(bio).length;
  return (
    <Section
      title="个人简介"
      icon={FileText}
      tone="neutral"
      description={bio ? `${count} / ${BIO_MAX} 字` : "用户还没有填写个人简介"}
      actions={
        <>
          {bio ? (
            <Button size="sm" variant="ghost" onClick={() => void copyToClipboard(bio, "个人简介")}>
              <ClipboardCopy />
              复制
            </Button>
          ) : null}
          <Button size="sm" variant="ghost" onClick={onEdit}>
            <Pencil />
            编辑
          </Button>
        </>
      }
    >
      {bio ? (
        <p className="max-h-72 overflow-y-auto text-sm leading-6 break-words whitespace-pre-wrap text-foreground/90">{bio}</p>
      ) : (
        <StateBlock
          compact
          icon={FileText}
          title="未填写个人简介"
          description="管理员可代为填写，保存后在客户端资料页展示。"
          action={
            <Button size="sm" variant="outline" onClick={onEdit}>
              <Pencil />
              填写简介
            </Button>
          }
        />
      )}
    </Section>
  );
}

// ── 注册来源 ──────────────────────────

function RegistrationSection({ user }: { user: AdminAppUserDetail }) {
  const profile = user.profile;
  const registeredAt = str(user.registerTime) || str(profile?.registerTime) || str(user.createdAt);
  const ip = str(user.registerIp) || str(profile?.registerIp);
  const place = [str(user.registerProvince) || str(profile?.registerProvince), str(user.registerCity) || str(profile?.registerCity)]
    .filter((v, i, arr) => v && arr.indexOf(v) === i)
    .join(" ");
  const isp = str(user.registerIsp) || str(profile?.registerIsp);
  const markcode = str(user.markcode) || str(profile?.markcode);
  const inviteCode = str(user.inviteCode) || str(profile?.inviteCode);
  const inviter = str(profile?.parentInviteAccount);

  const items: InfoItem[] = [
    {
      label: "注册时间",
      icon: CalendarClock,
      value: registeredAt ? formatDateTime(registeredAt) : undefined,
      hint: registeredAt ? formatRelative(registeredAt) : undefined
    },
    { label: "注册 IP", icon: Network, value: ip, mono: true, copy: ip || null },
    { label: "归属地", icon: MapPin, value: place, copy: place || null },
    { label: "运营商", icon: Network, value: isp },
    { label: "设备标识码", icon: Fingerprint, value: markcode, mono: true, copy: markcode || null },
    { label: "邀请码", icon: Ticket, value: inviteCode, mono: true, copy: inviteCode || null, hint: inviteCode ? "该用户自己的邀请码" : undefined },
    {
      label: "上级邀请人",
      icon: UserPlus,
      value: inviter,
      copy: inviter || null,
      hint: inviter ? "注册时填写的邀请人账号" : "自然注册，无邀请人"
    }
  ];

  const summary = [
    ["注册时间", registeredAt ? formatDateTime(registeredAt) : ""],
    ["注册 IP", ip],
    ["归属地", place],
    ["运营商", isp],
    ["设备标识码", markcode],
    ["邀请码", inviteCode],
    ["上级邀请人", inviter]
  ]
    .filter(([, value]) => value)
    .map(([label, value]) => `${label}：${value}`)
    .join("\n");

  return (
    <Section
      title="注册来源"
      icon={MapPin}
      tone="success"
      description="账号创建时记录的来源信息，只读"
      actions={
        summary ? (
          <Button size="sm" variant="outline" onClick={() => void copyToClipboard(summary, "注册来源")}>
            <ClipboardCopy />
            复制全部
          </Button>
        ) : null
      }
    >
      <InfoGrid items={items} columns={3} />
    </Section>
  );
}

// ── 附加字段 ──────────────────────────

function valueText(value: unknown) {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function ExtraFieldsSection({ user }: { user: AdminAppUserDetail }) {
  // 用户主表与资料表各有一份 extra，资料表优先
  const extra = useMemo<Record<string, unknown>>(
    () => ({ ...(user.extra ?? {}), ...(user.profile?.extra ?? {}) }),
    [user.extra, user.profile?.extra]
  );
  const entries = Object.entries(extra);
  const [mode, setMode] = useState<"list" | "json">("list");
  const [query, setQuery] = useState("");
  const json = useMemo(() => JSON.stringify(extra, null, 2), [extra]);

  const keyword = query.trim().toLowerCase();
  const filtered = keyword
    ? entries.filter(([key, value]) => key.toLowerCase().includes(keyword) || humanizeKey(key).toLowerCase().includes(keyword) || valueText(value).toLowerCase().includes(keyword))
    : entries;

  return (
    <Section
      title="附加字段"
      icon={Shapes}
      tone="violet"
      description={entries.length ? `应用自行写入的 ${entries.length} 个字段，只读` : "应用通过接口写入的自定义数据"}
      actions={
        entries.length ? (
          <>
            <ToggleGroup
              type="single"
              variant="outline"
              size="sm"
              value={mode}
              onValueChange={(value) => value && setMode(value as "list" | "json")}
              aria-label="显示方式"
            >
              <ToggleGroupItem value="list" aria-label="键值">
                <List />
                键值
              </ToggleGroupItem>
              <ToggleGroupItem value="json" aria-label="JSON">
                <Braces />
                JSON
              </ToggleGroupItem>
            </ToggleGroup>
            <Button size="sm" variant="ghost" onClick={() => void copyToClipboard(json, " JSON")}>
              <ClipboardCopy />
              复制 JSON
            </Button>
          </>
        ) : null
      }
      toolbar={
        entries.length > 6 && mode === "list" ? (
          <SearchField value={query} onChange={setQuery} placeholder="搜索字段名或值" />
        ) : undefined
      }
    >
      {!entries.length ? (
        <StateBlock compact icon={Shapes} title="没有附加字段" description="应用写入用户 extra 的自定义数据会显示在这里。" />
      ) : mode === "json" ? (
        <JsonViewer value={json} height={Math.min(480, Math.max(160, json.split("\n").length * 19 + 24))} />
      ) : filtered.length ? (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {filtered.map(([key, value]) => (
            <div key={key} className="flex min-w-0 flex-col gap-1 rounded-xl border bg-card px-4 py-3">
              <div className="flex items-start justify-between gap-2">
                <div className="flex min-w-0 flex-col">
                  <span className="truncate text-xs font-medium" title={humanizeKey(key)}>
                    {humanizeKey(key)}
                  </span>
                  <span className="truncate font-mono text-xs text-muted-foreground" title={key}>
                    {key}
                  </span>
                </div>
                <CopyButton value={valueText(value)} label={humanizeKey(key)} />
              </div>
              <div className="mt-1 min-w-0 text-sm">{readableValue(value)}</div>
            </div>
          ))}
        </div>
      ) : (
        <StateBlock compact title="没有匹配的字段" description="换个关键词试试。" />
      )}
    </Section>
  );
}
