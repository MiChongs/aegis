import { Loader2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

export function LoadingState({ title = "加载中", description = "正在拉取最新数据..." }: { title?: string; description?: string }) {
  return (
    <Card className="overflow-hidden">
      <CardContent className="flex min-h-64 flex-col justify-center gap-4 p-8">
        <Badge variant="outline" className="w-fit">
          <Loader2 className="mr-1 size-3 animate-spin" />
          处理中
        </Badge>
        <div className="space-y-2">
          <h3 className="text-lg font-semibold text-foreground">{title}</h3>
          <p className="max-w-xl text-sm leading-6 text-muted-foreground">{description}</p>
        </div>
        <div className="grid gap-3 md:grid-cols-3">
          <Skeleton className="h-20 rounded-2xl" />
          <Skeleton className="h-20 rounded-2xl" />
          <Skeleton className="h-20 rounded-2xl" />
        </div>
      </CardContent>
    </Card>
  );
}

export function EmptyState({ title, description }: { title: string; description: string }) {
  return (
    <Card className="overflow-hidden">
      <CardContent className="flex min-h-56 flex-col justify-center gap-3 p-8">
        <Badge variant="outline" className="w-fit">
          暂无数据
        </Badge>
        <h3 className="text-lg font-semibold text-foreground">{title}</h3>
        <p className="max-w-xl text-sm leading-6 text-muted-foreground">{description}</p>
      </CardContent>
    </Card>
  );
}
