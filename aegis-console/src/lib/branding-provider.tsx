"use client";

import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import type { BrandingConfig } from "@/lib/api/types";
import { getPublicBranding } from "@/lib/api/system";

const CACHE_KEY = "aegis-branding";

const defaults: BrandingConfig = {
  platformName: "Aegis",
  consoleName: "控制台",
  logoURL: "",
  logoDarkURL: "",
  faviconURL: "",
  primaryColor: "",
  primaryColorDark: "",
  accentColor: "",
  loginBgURL: "",
  loginBgColor: "",
  footerText: "",
  customCSS: "",
};

const BrandingContext = createContext<BrandingConfig>(defaults);

export function useBranding() {
  return useContext(BrandingContext);
}

// hex → oklch 近似（简化版：直接设置 hex 作为 CSS 变量）
function applyThemeColors(cfg: BrandingConfig) {
  const root = document.documentElement;

  if (cfg.primaryColor) {
    root.style.setProperty("--color-primary", cfg.primaryColor);
    // 自动计算前景色（深色用白，浅色用黑）
    const r = parseInt(cfg.primaryColor.slice(1, 3), 16);
    const g = parseInt(cfg.primaryColor.slice(3, 5), 16);
    const b = parseInt(cfg.primaryColor.slice(5, 7), 16);
    const lum = (0.299 * r + 0.587 * g + 0.114 * b) / 255;
    root.style.setProperty("--color-primary-foreground", lum > 0.5 ? "#09090b" : "#fafafa");
    root.style.setProperty("--color-ring", cfg.primaryColor);
    root.style.setProperty("--color-sidebar-primary", cfg.primaryColor);
    root.style.setProperty("--color-sidebar-ring", cfg.primaryColor);
  } else {
    root.style.removeProperty("--color-primary");
    root.style.removeProperty("--color-primary-foreground");
    root.style.removeProperty("--color-ring");
    root.style.removeProperty("--color-sidebar-primary");
    root.style.removeProperty("--color-sidebar-ring");
  }

  if (cfg.accentColor) {
    root.style.setProperty("--color-accent", cfg.accentColor);
  } else {
    root.style.removeProperty("--color-accent");
  }

  // 自定义 CSS 注入
  let styleEl = document.getElementById("aegis-custom-css");
  if (cfg.customCSS) {
    if (!styleEl) {
      styleEl = document.createElement("style");
      styleEl.id = "aegis-custom-css";
      document.head.appendChild(styleEl);
    }
    styleEl.textContent = cfg.customCSS;
  } else if (styleEl) {
    styleEl.remove();
  }

  // Favicon
  if (cfg.faviconURL) {
    let link = document.querySelector<HTMLLinkElement>("link[rel='icon']");
    if (!link) {
      link = document.createElement("link");
      link.rel = "icon";
      document.head.appendChild(link);
    }
    link.href = cfg.faviconURL;
  }
}

export function BrandingProvider({ children }: { children: ReactNode }) {
  const [branding, setBranding] = useState<BrandingConfig>(() => {
    if (typeof window === "undefined") return defaults;
    try {
      const cached = localStorage.getItem(CACHE_KEY);
      return cached ? { ...defaults, ...JSON.parse(cached) } : defaults;
    } catch {
      return defaults;
    }
  });

  useEffect(() => {
    getPublicBranding()
      .then((cfg) => {
        const merged = { ...defaults, ...cfg };
        setBranding(merged);
        localStorage.setItem(CACHE_KEY, JSON.stringify(merged));
        applyThemeColors(merged);
      })
      .catch(() => {
        applyThemeColors(branding);
      });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // 初次渲染也应用缓存的主题色
  useEffect(() => {
    applyThemeColors(branding);
  }, [branding]);

  return (
    <BrandingContext.Provider value={branding}>
      {children}
    </BrandingContext.Provider>
  );
}
