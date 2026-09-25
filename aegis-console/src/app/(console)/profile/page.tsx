"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { IdCard, KeyRound, ShieldCheck } from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api-client";
import {
  useAdminProfileQuery,
  useAdminRolePermissionTreeQuery,
  useAdminSessionQuery,
  useUpdateAdminProfileMutation,
  useUploadAdminAvatarMutation
} from "@/lib/admin-hooks";
import { useOperatorIdentity } from "@/lib/operator";
import type { AdminAccount } from "@/lib/api/types";
import { AutoSkeleton } from "@/components/ui/auto-skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { SectionHeading } from "@/components/ui/section-heading";
import { ProfileAccessPanel } from "@/components/profile/profile-access-panel";
import { ProfileAccountPanel } from "@/components/profile/profile-account-panel";
import { BasicInfoPanel, ContactsPanel } from "@/components/profile/profile-form-panels";
import { ProfileIdentityCard } from "@/components/profile/profile-identity-card";
import { ProfileSaveBar } from "@/components/profile/profile-save-bar";
import {
  changedFields,
  hasBlockingError,
  seedForm,
  toPayload,
  validateForm,
  type ProfileForm
} from "@/components/profile/profile-shared";

/**
 * 加载期间给骨架量尺寸用的占位账号。
 *
 * 骨架照着真实布局量出来，这份数据只负责把布局撑开：字段取接近真实值的长度，
 * 量出来的文字条才不会过长或过短。它在加载期间被渲染，但始终不可见
 * （见 components/ui/auto-skeleton.tsx）。
 */
const PLACEHOLDER_ACCOUNT: AdminAccount = {
  id: 0,
  account: "administrator",
  displayName: "Administrator",
  status: "active",
  authSource: "password",
  createdAt: "2026-01-01T00:00:00Z",
  lastLoginAt: "2026-01-01T00:00:00Z"
};

export default function ProfilePage() {
  const profileQuery = useAdminProfileQuery();
  const sessionQuery = useAdminSessionQuery();
  const roleTreeQuery = useAdminRolePermissionTreeQuery();
  const updateMutation = useUpdateAdminProfileMutation();
  const uploadMutation = useUploadAdminAvatarMutation();
  const operator = useOperatorIdentity();

  const account = profileQuery.data?.account;
  const assignments = useMemo(() => profileQuery.data?.assignments || [], [profileQuery.data]);

  /**
   * 草稿绑定服务端快照，**不用 useEffect 同步**（与 `/apps` 配置面板同一条约束）。
   * 没有草稿时直接从服务端派生，保存成功后 `setDraft(null)` 让它重新派生 ——
   * 用 effect 回灌既触发级联渲染、过不了 `react-hooks/set-state-in-effect`，
   * 也会让后台的一次静默刷新把你正在输入的内容冲掉。
   */
  const [draft, setDraft] = useState<ProfileForm | null>(null);
  const server = useMemo(() => seedForm(account), [account]);
  const form = draft ?? server;

  const issues = useMemo(() => validateForm(form, server), [form, server]);
  const changes = useMemo(() => changedFields(form, server), [form, server]);
  const blocked = hasBlockingError(issues);
  const dirty = draft !== null && (changes.length > 0 || Object.keys(issues.contactErrors).length > 0);

  const patch = useCallback(<K extends keyof ProfileForm>(key: K, value: ProfileForm[K]) => {
    setDraft((prev) => ({ ...(prev ?? server), [key]: value }));
  }, [server]);

  // 离开页面前拦一下。浏览器只允许弹它自己那句默认文案，但至少不会静默丢失
  useEffect(() => {
    if (!dirty) return;
    const onBeforeUnload = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [dirty]);

  const handleSave = useCallback(async () => {
    try {
      await updateMutation.mutateAsync(toPayload(form));
      setDraft(null);
      toast.success("资料已保存", {
        description: changes.length ? `已更新：${changes.join("、")}` : undefined
      });
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }, [form, changes, updateMutation]);

  const handleUpload = useCallback(
    async (file: File) => {
      try {
        await uploadMutation.mutateAsync({ file });
        toast.success("头像已更新");
      } catch (error) {
        toast.error(error instanceof ApiError ? error.message : "头像上传失败");
      }
    },
    [uploadMutation]
  );

  // 加载期间照常渲染真实布局（喂占位账号），骨架由 AutoSkeleton 照着它量出来。
  // 不再手写一份「长得像」的骨架：那份和真实布局迟早对不上。
  const loading = profileQuery.isLoading || !account;
  const shown = account ?? PLACEHOLDER_ACCOUNT;

  return (
    <div className="page-stack pb-4">
      <SectionHeading eyebrow="控制台" title="个人资料" />

      <AutoSkeleton loading={loading}>
        {/* 间距要写在包进来的这一层：page-stack 的 gap 只作用于它的直接子元素 */}
        <div className="flex flex-col gap-5">
          <ProfileIdentityCard
            account={shown}
            avatarSrc={operator.avatarSrc}
            uploading={uploadMutation.isPending}
            onUpload={handleUpload}
          />

          <Tabs defaultValue="profile" className="gap-4">
            <TabsList>
              <TabsTrigger value="profile" className="gap-1.5 text-xs">
                <IdCard className="size-3.5" />
                资料
                {dirty ? <span className="size-1.5 rounded-full bg-amber-500" aria-label="有未保存改动" /> : null}
              </TabsTrigger>
              <TabsTrigger value="access" className="gap-1.5 text-xs">
                <KeyRound className="size-3.5" />
                角色与权限
              </TabsTrigger>
              <TabsTrigger value="account" className="gap-1.5 text-xs">
                <ShieldCheck className="size-3.5" />
                账户与会话
              </TabsTrigger>
            </TabsList>

            <TabsContent value="profile" className="space-y-4">
              <BasicInfoPanel form={form} issues={issues} patch={patch} />
              <ContactsPanel form={form} issues={issues} patch={patch} />
            </TabsContent>

            <TabsContent value="access">
              <ProfileAccessPanel
                isSuperAdmin={Boolean(shown.isSuperAdmin)}
                assignments={assignments}
                roleTree={roleTreeQuery.data}
                loading={roleTreeQuery.isLoading}
              />
            </TabsContent>

            <TabsContent value="account">
              <ProfileAccountPanel
                account={shown}
                session={sessionQuery.data}
                sessionLoading={sessionQuery.isLoading}
                loginAvailability={profileQuery.data?.loginAvailability}
              />
            </TabsContent>
          </Tabs>
        </div>
      </AutoSkeleton>

      <ProfileSaveBar
        visible={dirty}
        changes={changes}
        blocked={blocked}
        saving={updateMutation.isPending}
        onSave={handleSave}
        onDiscard={() => setDraft(null)}
      />
    </div>
  );
}
