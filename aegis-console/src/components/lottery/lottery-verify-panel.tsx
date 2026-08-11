"use client";

import { useState } from "react";
import { CheckCircle2, ExternalLink, Loader2, ShieldCheck, XCircle } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useAdminToken } from "@/lib/admin-hooks";
import { verifyLotteryActivity } from "@/lib/api/lottery";
import type { LotteryVerifyResult } from "@/lib/api/lottery";

export function LotteryVerifyPanel({ activityId }: { activityId: number }) {
  const token = useAdminToken();
  const [verifying, setVerifying] = useState(false);
  const [result, setResult] = useState<LotteryVerifyResult | null>(null);

  async function handleVerify() {
    if (!token) return;
    setVerifying(true);
    try {
      const res = await verifyLotteryActivity(token, activityId);
      setResult(res);
      if (res.valid) {
        toast.success("验证通过");
      } else {
        toast.error("验证失败: " + res.message);
      }
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "验证失败");
    } finally {
      setVerifying(false);
    }
  }

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm">
          <ShieldCheck className="size-4" />
          公平性验证
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {result && (
          <div className="space-y-3">
            {/* 验证结果 */}
            <div className="flex items-center gap-2">
              {result.valid ? (
                <>
                  <CheckCircle2 className="size-5 text-emerald-600 dark:text-emerald-400" />
                  <span className="text-sm font-medium text-emerald-700 dark:text-emerald-300">验证通过</span>
                </>
              ) : (
                <>
                  <XCircle className="size-5 text-red-600 dark:text-red-400" />
                  <span className="text-sm font-medium text-red-700 dark:text-red-300">验证失败</span>
                </>
              )}
              <Badge variant={result.valid ? "success" : "danger"} className="ml-auto">
                {result.valid ? "有效" : "无效"}
              </Badge>
            </div>

            {/* 详细信息 */}
            <div className="space-y-2 rounded-xl border border-border/60 bg-muted/30 p-3">
              <InfoRow label="种子哈希" value={result.seedHash} mono />
              {result.seedValue && (
                <InfoRow label="种子值" value={result.seedValue} mono />
              )}
              {result.chainTxHash && (
                <div className="flex items-start gap-2 text-xs">
                  <span className="shrink-0 text-muted-foreground">链上哈希:</span>
                  <a
                    href={getExplorerUrl(result.chainNetwork, result.chainTxHash)}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 break-all font-mono text-primary hover:underline"
                  >
                    {result.chainTxHash}
                    <ExternalLink className="size-3 shrink-0" />
                  </a>
                </div>
              )}
              {result.message && (
                <InfoRow label="消息" value={result.message} />
              )}
            </div>
          </div>
        )}

        <Button
          size="sm"
          variant="outline"
          className="h-8 gap-1 text-xs"
          disabled={verifying}
          onClick={handleVerify}
        >
          {verifying ? (
            <>
              <Loader2 className="size-3 animate-spin" />
              验证中...
            </>
          ) : (
            <>
              <ShieldCheck className="size-3" />
              验证公平性
            </>
          )}
        </Button>
      </CardContent>
    </Card>
  );
}

function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-start gap-2 text-xs">
      <span className="shrink-0 text-muted-foreground">{label}:</span>
      <span className={`break-all text-foreground ${mono ? "font-mono" : ""}`}>{value}</span>
    </div>
  );
}

function getExplorerUrl(network?: string | null, txHash?: string) {
  if (!txHash) return "#";
  switch (network) {
    case "ethereum":
      return `https://etherscan.io/tx/${txHash}`;
    case "bsc":
      return `https://bscscan.com/tx/${txHash}`;
    case "polygon":
      return `https://polygonscan.com/tx/${txHash}`;
    default:
      return `https://etherscan.io/tx/${txHash}`;
  }
}
