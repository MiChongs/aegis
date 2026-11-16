"use client";

import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { WalletPackagesSection } from "./wallet-packages-section";
import { WalletSettingsSection } from "./wallet-settings-section";

/** 设置与档位的写入权限点（权限树「交易 → 钱包 → 钱包设置与充值档位」）；读走 app:wallet:read */
export const WALLET_CONFIG_PERMISSION = "app:wallet:config";

/**
 * 交易中心「钱包设置」页签：钱包设置 + 充值档位（docs/wallet.md）。
 *
 * 没有 `app:wallet:config` 时整页只读：设置的输入框与开关置灰、不出保存按钮，
 * 档位不出新建、排序、上下架与菜单 —— 点了也只会被后端 403 拒掉。
 */
export function WalletConfigPanel({ appKey, walletCurrency }: { appKey?: string | null; walletCurrency: string }) {
  const can = useAppKeyPermissionChecker(appKey);
  const canConfig = can(WALLET_CONFIG_PERMISSION);
  if (!appKey) return null;
  return (
    <div className="flex flex-col gap-4">
      <WalletSettingsSection appKey={appKey} canConfig={canConfig} />
      <WalletPackagesSection appKey={appKey} currency={walletCurrency} canConfig={canConfig} />
    </div>
  );
}
