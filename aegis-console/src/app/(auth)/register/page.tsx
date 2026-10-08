import type { Metadata } from "next";
import { AuthShell } from "@/components/auth/auth-shell";
import { RegisterForm } from "@/components/auth/register-form";

export const metadata: Metadata = {
  title: "注册 · Aegis Console",
  description: "创建 Aegis 管理控制台账号"
};

/** 注册页。与登录页共用 AuthShell，两页互相跳转时只有表单内容在变。 */
export default function RegisterPage() {
  return (
    <AuthShell>
      <RegisterForm />
    </AuthShell>
  );
}
