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
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
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
      toast.error(error instanceof ApiError ? error.message : "保存失败，请稍后重试");
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

  if (profileQuery.isLoading || !account) return <ProfileSkeleton />;

  return (
    <div className="page-stack pb-4">
      <SectionHeading eyebrow="控制台" title="个人资料" />

      <ProfileIdentityCard
        account={account}
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
            isSuperAdmin={Boolean(account.isSuperAdmin)}
            assignments={assignments}
            roleTree={roleTreeQuery.data}
            loading={roleTreeQuery.isLoading}
          />
        </TabsContent>

        <TabsContent value="account">
          <ProfileAccountPanel
            account={account}
            session={sessionQuery.data}
            sessionLoading={sessionQuery.isLoading}
            loginAvailability={profileQuery.data?.loginAvailability}
          />
        </TabsContent>
      </Tabs>

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

/* ------------------------------------------------------------------ */
/*  骨架屏：按真实布局排，避免加载完成时整页跳一下                          */
/* ------------------------------------------------------------------ */

function ProfileSkeleton() {
  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="个人资料" />
      <Card className="py-0">
        <CardContent className="flex items-center gap-5 p-5">
          <Skeleton className="size-20 shrink-0 rounded-2xl" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-6 w-48" />
            <Skeleton className="h-4 w-32" />
            <Skeleton className="h-4 w-72" />
          </div>
        </CardContent>
      </Card>
      <Skeleton className="h-9 w-72 rounded-lg" />
      <Card className="py-0">
        <CardContent className="space-y-4 p-5">
          <Skeleton className="h-5 w-24" />
          <div className="grid gap-4 sm:grid-cols-2">
            <Skeleton className="h-14" />
            <Skeleton className="h-14" />
            <Skeleton className="h-14" />
            <Skeleton className="h-14" />
          </div>
          <Skeleton className="h-20" />
        </CardContent>
      </Card>
    </div>
  );
}
