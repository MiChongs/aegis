#!/usr/bin/env python3
"""生成 README 头图（.github/assets/aegis-banner-{zh,en,ja}.svg）。

三种语言共用同一套版式：左侧是品牌、定位与能力标签，右侧是一张请求流向架构图
（接入方 → 边界防护 → Go 运行时 → 数据面，下方是风控微服务平台）。
纯平面配色，不用渐变；文字用系统字体栈，GitHub 以 <img> 渲染 SVG 时也能正常显示。

用法：python3 scripts/banner/generate.py
"""
from __future__ import annotations

import pathlib
from xml.sax.saxutils import escape

ROOT = pathlib.Path(__file__).resolve().parents[2]
OUT = ROOT / ".github" / "assets"

W, H = 1600, 560

# 配色：深色底板 + 四种功能色（与控制台的语义色同族）
BG = "#0B1120"
PANEL = "#111A2E"
PANEL_2 = "#16213A"
LINE = "#26324D"
TEXT = "#F1F5F9"
MUTED = "#94A3B8"
DIM = "#64748B"
TEAL = "#2DD4BF"
BLUE = "#60A5FA"
AMBER = "#FBBF24"
ROSE = "#FB7185"
VIOLET = "#A78BFA"

SANS = "Inter, 'PingFang SC', 'Hiragino Sans', 'Noto Sans CJK SC', 'Microsoft YaHei', 'Segoe UI', sans-serif"
MONO = "'JetBrains Mono', 'SFMono-Regular', Menlo, Consolas, monospace"

COPY = {
    "zh": {
        "lang": "zh-CN",
        "eyebrow": "多租户用户平台",
        "tagline": ["一个 Go 运行时承载全部业务能力，", "一组独立微服务做毫秒级实时风控。"],
        "chips": ["认证与会话", "资产与交易", "内容与触达", "远程函数", "AI 网关", "实时风控"],
        "clients": ["官方 SDK", "接入方服务端", "管理控制台"],
        "edge_title": "边界",
        "edge": ["真实客户端 IP", "Coraza WAF", "流量管理", "网关验签解包"],
        "core_title": "Go 运行时",
        "core": ["领域服务", "Worker", "实时中心"],
        "risk_title": "风控平台",
        "risk_link": "Kitex RPC",
        "risk": ["决策 Kitex", "管理 Hertz", "关系图 Nebula", "向量 Milvus", "Flink CEP", "ClickHouse"],
    },
    "en": {
        "lang": "en",
        "eyebrow": "MULTI-TENANT USER PLATFORM",
        "tagline": ["One Go runtime for every business capability,", "microservices for millisecond risk decisions."],
        "chips": ["Identity", "Commerce", "Messaging", "Functions", "AI Gateway", "Realtime Risk"],
        "clients": ["Official SDK", "Partner Backend", "Admin Console"],
        "edge_title": "Edge",
        "edge": ["Real Client IP", "Coraza WAF", "Traffic Control", "Gateway Verify"],
        "core_title": "Go Runtime",
        "core": ["Domain Services", "Worker", "Realtime Hub"],
        "risk_title": "Risk Platform",
        "risk_link": "Kitex RPC",
        "risk": ["Decision Kitex", "Admin Hertz", "Graph Nebula", "Vector Milvus", "Flink CEP", "ClickHouse"],
    },
    "ja": {
        "lang": "ja",
        "eyebrow": "マルチテナント ユーザープラットフォーム",
        "tagline": ["ひとつの Go ランタイムが全業務を担い、", "マイクロサービスがミリ秒で審査する。"],
        "chips": ["認証とセッション", "資産と決済", "コンテンツ配信", "リモート関数", "AI ゲートウェイ", "リアルタイム審査"],
        "clients": ["公式 SDK", "連携先サーバー", "管理コンソール"],
        "edge_title": "エッジ",
        "edge": ["実クライアント IP", "Coraza WAF", "トラフィック制御", "ゲートウェイ検証"],
        "core_title": "Go ランタイム",
        "core": ["ドメインサービス", "Worker", "リアルタイムハブ"],
        "risk_title": "リスク基盤",
        "risk_link": "Kitex RPC",
        "risk": ["判定 Kitex", "管理 Hertz", "グラフ Nebula", "ベクトル Milvus", "Flink CEP", "ClickHouse"],
    },
}

DATA = [("PostgreSQL 17", BLUE), ("Redis 8", ROSE), ("NATS JetStream", TEAL), ("Temporal", VIOLET)]


def text_width(s: str, size: float) -> float:
    """估算文字宽度：全角字符按 1 个字号，半角按 0.58 个字号。"""
    w = 0.0
    for ch in s:
        w += size if ord(ch) > 0x2E80 else size * 0.58
    return w


def t(x, y, s, size, fill, weight=400, family=SANS, anchor="start", spacing=None) -> str:
    ls = f' letter-spacing="{spacing}"' if spacing is not None else ""
    return (f'<text x="{x:.1f}" y="{y:.1f}" font-family="{family}" font-size="{size}" font-weight="{weight}" '
            f'fill="{fill}" text-anchor="{anchor}"{ls}>{escape(s)}</text>')


def rect(x, y, w, h, fill, rx=12, stroke=None, sw=1, dash=None) -> str:
    st = f' stroke="{stroke}" stroke-width="{sw}"' if stroke else ""
    da = f' stroke-dasharray="{dash}"' if dash else ""
    return f'<rect x="{x:.1f}" y="{y:.1f}" width="{w:.1f}" height="{h:.1f}" rx="{rx}" fill="{fill}"{st}{da}/>'


def arrow(x1, y1, x2, y2, color=LINE, dash=None) -> str:
    da = f' stroke-dasharray="{dash}"' if dash else ""
    mid = (x1 + x2) / 2
    return (f'<path d="M{x1:.1f} {y1:.1f} C{mid:.1f} {y1:.1f} {mid:.1f} {y2:.1f} {x2:.1f} {y2:.1f}" '
            f'stroke="{color}" stroke-width="2" fill="none"{da} marker-end="url(#arrow)"/>')


def shield(cx, cy, s, color) -> str:
    """盾牌标志：Aegis 本义是宙斯的神盾。"""
    p = (f"M{cx} {cy - s} L{cx + s * 0.86} {cy - s * 0.62} L{cx + s * 0.86} {cy + s * 0.08} "
         f"C{cx + s * 0.86} {cy + s * 0.62} {cx + s * 0.42} {cy + s * 0.92} {cx} {cy + s * 1.1} "
         f"C{cx - s * 0.42} {cy + s * 0.92} {cx - s * 0.86} {cy + s * 0.62} {cx - s * 0.86} {cy + s * 0.08} "
         f"L{cx - s * 0.86} {cy - s * 0.62} Z")
    inner = (f"M{cx - s * 0.36} {cy + s * 0.04} L{cx - s * 0.08} {cy + s * 0.34} L{cx + s * 0.42} {cy - s * 0.24}")
    return (f'<path d="{p}" fill="{color}"/>'
            f'<path d="{inner}" stroke="{BG}" stroke-width="{s * 0.16:.1f}" stroke-linecap="round" stroke-linejoin="round" fill="none"/>')


def build(lang: str) -> str:
    c = COPY[lang]
    out: list[str] = []
    out.append(f'<svg width="{W}" height="{H}" viewBox="0 0 {W} {H}" fill="none" xmlns="http://www.w3.org/2000/svg" '
               f'role="img" aria-label="Aegis" xml:lang="{c["lang"]}">')
    out.append("<defs>"
               f'<pattern id="dots" width="24" height="24" patternUnits="userSpaceOnUse"><circle cx="2" cy="2" r="1.2" fill="{LINE}"/></pattern>'
               f'<marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">'
               f'<path d="M0 0 L10 5 L0 10 Z" fill="{DIM}"/></marker>'
               "</defs>")
    out.append(rect(0, 0, W, H, BG, rx=28))
    out.append(f'<rect x="0" y="0" width="{W}" height="{H}" rx="28" fill="url(#dots)" opacity="0.55"/>')

    # ── 左侧：品牌 ──
    lx = 80
    out.append(shield(lx + 30, 118, 30, TEAL))
    eb = c["eyebrow"]
    ebw = text_width(eb, 15) + (len(eb) * 1.6 if lang == "en" else 0) + 30
    out.append(rect(lx + 80, 96, ebw, 34, PANEL_2, rx=17, stroke=LINE))
    out.append(t(lx + 94, 119, eb, 15, TEAL, 600, spacing=1.2 if lang == "en" else None))
    out.append(t(lx - 4, 262, "Aegis", 118, TEXT, 800, spacing=-3))
    for i, line in enumerate(c["tagline"]):
        out.append(t(lx, 318 + i * 38, line, {"zh": 25, "en": 22, "ja": 23}[lang], MUTED, 400))

    # 能力标签：两行三列
    cx0, cy0 = lx, 410
    col_w = 186
    palette = [TEAL, BLUE, AMBER, VIOLET, ROSE, TEAL]
    for i, chip in enumerate(c["chips"]):
        r, k = divmod(i, 3)
        x, y = cx0 + k * col_w, cy0 + r * 52
        w = col_w - 14
        out.append(rect(x, y, w, 38, PANEL, rx=10, stroke=LINE))
        out.append(f'<circle cx="{x + 18}" cy="{y + 19}" r="5" fill="{palette[i]}"/>')
        size = 16 if text_width(chip, 16) < w - 44 else 14
        out.append(t(x + 32, y + 25, chip, size, TEXT, 500))

    # ── 右侧：架构图 ──
    # 列 1：接入方
    c1x, c1w = 700, 176
    client_ys = [118, 196, 274]
    for y, name in zip(client_ys, c["clients"]):
        out.append(rect(c1x, y, c1w, 54, PANEL, rx=12, stroke=LINE))
        out.append(f'<rect x="{c1x}" y="{y + 14}" width="4" height="26" rx="2" fill="{BLUE}"/>')
        out.append(t(c1x + 20, y + 33, name, 17 if text_width(name, 17) < c1w - 30 else 15, TEXT, 500))

    # 列 2：边界
    c2x, c2w, c2y, c2h = 930, 186, 98, 250
    out.append(rect(c2x, c2y, c2w, c2h, PANEL, rx=16, stroke=AMBER, sw=1.5))
    out.append(shield(c2x + 26, c2y + 30, 11, AMBER))
    out.append(t(c2x + 46, c2y + 36, c["edge_title"], 18, AMBER, 700))
    for i, layer in enumerate(c["edge"]):
        y = c2y + 58 + i * 46
        out.append(rect(c2x + 14, y, c2w - 28, 36, PANEL_2, rx=8))
        out.append(t(c2x + 26, y + 23, f"{i + 1}", 13, DIM, 700, MONO))
        size = 15 if text_width(layer, 15) < c2w - 72 else 13
        out.append(t(c2x + 44, y + 23, layer, size, TEXT, 500))

    # 列 3：Go 运行时
    c3x, c3w, c3y, c3h = 1170, 196, 98, 250
    out.append(rect(c3x, c3y, c3w, c3h, PANEL, rx=16, stroke=TEAL, sw=1.5))
    out.append(t(c3x + 18, c3y + 36, c["core_title"], 18, TEAL, 700))
    out.append(t(c3x + 18, c3y + 60, "Gin + Uber Fx", 13, DIM, 500, MONO))
    for i, name in enumerate(c["core"]):
        y = c3y + 78 + i * 52
        out.append(rect(c3x + 14, y, c3w - 28, 42, PANEL_2, rx=8))
        out.append(f'<circle cx="{c3x + 32}" cy="{y + 21}" r="4" fill="{TEAL}"/>')
        size = 15 if text_width(name, 15) < c3w - 70 else 13
        out.append(t(c3x + 46, y + 26, name, size, TEXT, 500))

    # 列 4：数据面
    c4x, c4w = 1404, 146
    data_ys = [98, 164, 230, 296]
    for y, (name, col) in zip(data_ys, DATA):
        out.append(rect(c4x, y, c4w, 52, PANEL, rx=10, stroke=LINE))
        out.append(f'<rect x="{c4x}" y="{y + 13}" width="4" height="26" rx="2" fill="{col}"/>')
        out.append(t(c4x + 16, y + 31, name, 13 if len(name) > 10 else 14, TEXT, 600, MONO))

    # 连线
    for y in client_ys:
        out.append(arrow(c1x + c1w, y + 27, c2x - 4, c2y + c2h / 2))
    out.append(arrow(c2x + c2w, c2y + c2h / 2, c3x - 4, c3y + c3h / 2, AMBER))
    for y in data_ys:
        out.append(arrow(c3x + c3w, c3y + c3h / 2, c4x - 4, y + 26))

    # 下方：风控平台
    rx0, ry0, rw, rh = 930, 392, 620, 108
    out.append(rect(rx0, ry0, rw, rh, PANEL, rx=16, stroke=ROSE, sw=1.5, dash="6 5"))
    out.append(t(rx0 + 18, ry0 + 32, c["risk_title"], 18, ROSE, 700))
    out.append(arrow(c3x + c3w / 2, c3y + c3h, c3x + c3w / 2, ry0 - 4, ROSE, dash="5 4"))
    out.append(t(c3x + c3w / 2 + 10, (c3y + c3h + ry0) / 2 + 5, c["risk_link"], 12, ROSE, 600, MONO))
    gx, gy, gw, gh, gap = rx0 + 18, ry0 + 48, 92, 42, 8
    gw = (rw - 36 - gap * 5) / 6
    for i, name in enumerate(c["risk"]):
        x = gx + i * (gw + gap)
        out.append(rect(x, gy, gw, gh, PANEL_2, rx=8))
        parts = name.split(" ", 1)
        if len(parts) == 2 and parts[0] != "Flink":
            out.append(t(x + gw / 2, gy + 17, parts[0], 12, MUTED, 500, anchor="middle"))
            out.append(t(x + gw / 2, gy + 34, parts[1], 13, TEXT, 600, MONO, anchor="middle"))
        else:
            out.append(t(x + gw / 2, gy + 26, name, 13, TEXT, 600, MONO, anchor="middle"))

    out.append("</svg>")
    return "\n".join(out) + "\n"


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    for lang in COPY:
        path = OUT / f"aegis-banner-{lang}.svg"
        path.write_text(build(lang), encoding="utf-8")
        print(path.relative_to(ROOT))


if __name__ == "__main__":
    main()
