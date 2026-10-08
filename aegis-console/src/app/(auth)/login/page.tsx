import type { Metadata } from "next";
import { AuthShell } from "@/components/auth/auth-shell";
import { LoginForm } from "@/components/auth/login-form";

export const metadata: Metadata = {
  title: "登录 · Aegis Console",
  description: "Aegis 管理控制台登录入口"
};

/** 登录页。版式见 AuthShell：桌面端左右分栏，移动端单栏。 */
export default function LoginPage() {
  return (
    <AuthShell>
      <LoginForm />
    </AuthShell>
  );
}
