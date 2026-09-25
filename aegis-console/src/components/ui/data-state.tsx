import { Loader2 } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

export function LoadingState({ title = "加载中", description }: { title?: string; description?: string }) {
  return (
    <Card className="overflow-hidden">
      <CardContent className="flex min-h-64 flex-col justify-center gap-4 p-8">
        <div className="space-y-2">
          <h3 className="flex items-center gap-2 text-lg font-semibold text-foreground">
            <Loader2 className="size-4 animate-spin text-muted-foreground" />
            {title}
          </h3>
          {description ? <p className="max-w-xl text-sm leading-6 text-muted-foreground">{description}</p> : null}
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

export function EmptyState({ title, description }: { title: string; description?: string }) {
  return (
    <Card className="overflow-hidden">
      <CardContent className="flex min-h-56 flex-col justify-center gap-3 p-8">
        <h3 className="text-lg font-semibold text-foreground">{title}</h3>
        {description ? <p className="max-w-xl text-sm leading-6 text-muted-foreground">{description}</p> : null}
      </CardContent>
    </Card>
  );
}
