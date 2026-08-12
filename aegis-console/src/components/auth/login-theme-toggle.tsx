"use client";

import { Moon, Sun } from "lucide-react";
import { AnimatePresence, m, useReducedMotion } from "motion/react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useIsClient } from "@/lib/use-client-value";
import { useThemeTransition } from "@/lib/theme-transition";

/**
 * 登录页的深浅色切换。
 *
 * 放在登录页而不是等进了控制台再切：登录页是唯一一个**未登录也会看到**的
 * 长时间停留界面，浅色党在深色屏幕前填密码是实打实的难受。
 *
 * 服务端渲染阶段主题未知，先占位同尺寸空盒 —— 与 `PortalShell` 同一处理，
 * 否则首帧图标会闪一次错的。
 */
export function LoginThemeToggle() {
  const { isDark, toggle } = useThemeTransition();
  const isClient = useIsClient();
  const reduced = useReducedMotion();

  if (!isClient) return <span className="size-9" aria-hidden />;

  const dark = isDark;
  const label = dark ? "切换到浅色主题" : "切换到深色主题";

  return (
    <TooltipProvider delayDuration={200}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label={label}
            className="relative overflow-hidden text-muted-foreground hover:text-foreground"
            onClick={(event) => toggle(event)}
          >
            <AnimatePresence mode="wait" initial={false}>
              <m.span
                key={dark ? "sun" : "moon"}
                initial={reduced ? false : { opacity: 0, rotate: -70, scale: 0.6 }}
                animate={{ opacity: 1, rotate: 0, scale: 1 }}
                exit={reduced ? undefined : { opacity: 0, rotate: 70, scale: 0.6 }}
                transition={{ duration: 0.22, ease: [0.32, 0.72, 0, 1] }}
                className="flex items-center justify-center"
              >
                {dark ? <Sun className="size-4" /> : <Moon className="size-4" />}
              </m.span>
            </AnimatePresence>
          </Button>
        </TooltipTrigger>
        <TooltipContent side="bottom">{label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
