import { ReactNode } from "react";
import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export function SurfaceCard({
  className,
  children
}: {
  className?: string;
  children: ReactNode;
}) {
  return (
    <Card className={cn("overflow-hidden", className)}>
      <CardContent className="p-6">{children}</CardContent>
    </Card>
  );
}
