/**
 * 浏览器设备指纹采集 SDK
 * 在登录时采集并随请求提交，用于风控评估
 */

export type BrowserFingerprint = {
  canvasHash: string;
  webglRenderer: string;
  webglVendor: string;
  screenWidth: number;
  screenHeight: number;
  colorDepth: number;
  timezone: string;
  timezoneOffset: number;
  language: string;
  languages: string[];
  platform: string;
  hardwareConcurrency: number;
  maxTouchPoints: number;
  cookieEnabled: boolean;
  doNotTrack: string;
  deviceMemory: number;
  plugins: string[];
  audioHash: string;
};

/** 采集浏览器指纹（约 50ms） */
export async function collectFingerprint(): Promise<BrowserFingerprint> {
  const fp: BrowserFingerprint = {
    canvasHash: getCanvasHash(),
    webglRenderer: "",
    webglVendor: "",
    screenWidth: screen.width,
    screenHeight: screen.height,
    colorDepth: screen.colorDepth,
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    timezoneOffset: new Date().getTimezoneOffset(),
    language: navigator.language,
    languages: Array.from(navigator.languages || []),
    platform: navigator.platform || "",
    hardwareConcurrency: navigator.hardwareConcurrency || 0,
    maxTouchPoints: navigator.maxTouchPoints || 0,
    cookieEnabled: navigator.cookieEnabled,
    doNotTrack: navigator.doNotTrack || "",
    deviceMemory: (navigator as unknown as Record<string, number>).deviceMemory || 0,
    plugins: getPlugins(),
    audioHash: "",
  };

  // WebGL
  try {
    const canvas = document.createElement("canvas");
    const gl = canvas.getContext("webgl") || canvas.getContext("experimental-webgl");
    if (gl) {
      const glCtx = gl as WebGLRenderingContext;
      const dbg = glCtx.getExtension("WEBGL_debug_renderer_info");
      if (dbg) {
        fp.webglRenderer = glCtx.getParameter(dbg.UNMASKED_RENDERER_WEBGL) || "";
        fp.webglVendor = glCtx.getParameter(dbg.UNMASKED_VENDOR_WEBGL) || "";
      }
    }
  } catch {}

  // 音频指纹
  try {
    fp.audioHash = await getAudioHash();
  } catch {}

  return fp;
}

/** 生成复合设备 ID（SHA-256 hash） */
export async function generateDeviceId(fp: BrowserFingerprint): Promise<string> {
  const raw = [fp.canvasHash, fp.webglRenderer, fp.screenWidth, fp.screenHeight, fp.timezone, fp.language, fp.platform, fp.hardwareConcurrency].join("|");
  const buf = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(raw));
  return Array.from(new Uint8Array(buf)).map((b) => b.toString(16).padStart(2, "0")).join("");
}

function getCanvasHash(): string {
  try {
    const canvas = document.createElement("canvas");
    canvas.width = 200;
    canvas.height = 50;
    const ctx = canvas.getContext("2d");
    if (!ctx) return "";
    ctx.textBaseline = "top";
    ctx.font = "14px Arial";
    ctx.fillStyle = "#f60";
    ctx.fillRect(125, 1, 62, 20);
    ctx.fillStyle = "#069";
    ctx.fillText("Aegis FP", 2, 15);
    ctx.fillStyle = "rgba(102, 204, 0, 0.7)";
    ctx.fillText("Aegis FP", 4, 17);
    return simpleHash(canvas.toDataURL());
  } catch {
    return "";
  }
}

function getPlugins(): string[] {
  try {
    return Array.from(navigator.plugins || []).slice(0, 10).map((p) => p.name);
  } catch {
    return [];
  }
}

async function getAudioHash(): Promise<string> {
  return new Promise((resolve) => {
    try {
      const ctx = new (window.AudioContext || (window as unknown as Record<string, typeof AudioContext>).webkitAudioContext)();
      const osc = ctx.createOscillator();
      const analyser = ctx.createAnalyser();
      const gain = ctx.createGain();
      const processor = ctx.createScriptProcessor(4096, 1, 1);

      gain.gain.value = 0;
      osc.type = "triangle";
      osc.frequency.value = 10000;
      osc.connect(analyser);
      analyser.connect(processor);
      processor.connect(gain);
      gain.connect(ctx.destination);

      let result = "";
      processor.onaudioprocess = (e) => {
        const data = new Float32Array(analyser.frequencyBinCount);
        analyser.getFloatFrequencyData(data);
        result = simpleHash(data.slice(0, 30).join(","));
        processor.disconnect();
        gain.disconnect();
        osc.disconnect();
        ctx.close();
        resolve(result);
      };
      osc.start(0);

      setTimeout(() => resolve(result || "timeout"), 500);
    } catch {
      resolve("");
    }
  });
}

function simpleHash(str: string): string {
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    const chr = str.charCodeAt(i);
    hash = ((hash << 5) - hash) + chr;
    hash |= 0;
  }
  return Math.abs(hash).toString(36);
}
