import { AuthGate } from "@/components/auth/auth-gate";
import { ConsoleShell } from "@/components/layout/console-shell";
import { PermissionProvider } from "@/lib/permissions";

export default function ConsoleLayout({ children }: { children: React.ReactNode }) {
  return (
    <AuthGate>
      <PermissionProvider>
        <ConsoleShell>{children}</ConsoleShell>
      </PermissionProvider>
    </AuthGate>
  );
}
