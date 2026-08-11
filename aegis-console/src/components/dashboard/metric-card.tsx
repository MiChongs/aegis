import { TrendingUp } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";

export function MetricCard({
  label,
  value,
  delta,
  detail
}: {
  label: string;
  value: string;
  delta: string;
  detail: string;
}) {
  return (
    <Card className="overflow-hidden">
      <CardContent className="space-y-4 p-5">
        <div className="flex items-center justify-between gap-3">
          <span className="text-sm font-medium text-muted-foreground">{label}</span>
          <Badge variant="outline">实时</Badge>
        </div>
        <div className="space-y-1">
          <strong className="block text-3xl font-semibold tracking-tight text-foreground">{value}</strong>
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <TrendingUp className="size-4 text-primary" />
            <span className="font-medium text-foreground">{delta}</span>
          </div>
        </div>
        <p className="text-sm text-muted-foreground">{detail}</p>
      </CardContent>
    </Card>
  );
}
