"""
RDKit 手性碳验证码微服务
POST /generate?width=800&height=600
渲染风格：骨架式，不显式标注手性碳，叠加边缘干扰和轻度防机器识别扰动
"""

import base64
import io
import math
import random
import traceback

import cairosvg
from flask import Flask, jsonify, request
from PIL import Image, ImageChops, ImageDraw, ImageFilter, ImageOps
from rdkit import Chem
from rdkit.Chem import AllChem, rdDepictor
from rdkit.Chem.Draw import rdMolDraw2D

app = Flask(__name__)

rdDepictor.SetPreferCoordGen(True)

RESAMPLING = Image.Resampling.LANCZOS
ATOM_PALETTE = {
    6: (0.16, 0.18, 0.22),
    7: (0.11, 0.33, 0.74),
    8: (0.83, 0.28, 0.22),
    9: (0.23, 0.45, 0.76),
    15: (0.66, 0.38, 0.16),
    16: (0.75, 0.61, 0.16),
    17: (0.12, 0.55, 0.32),
    35: (0.63, 0.31, 0.18),
}

RENDER_THEMES = [
    {
        "bg_top": (250, 247, 241),
        "bg_bottom": (236, 232, 224),
        "panel_fill": (255, 252, 248),
        "panel_edge": (205, 193, 177),
        "accent": (208, 182, 139),
        "accent_soft": (244, 231, 204),
        "decoy_tint": (123, 129, 141),
        "noise_ink": (173, 178, 186),
        "token_ink": (132, 136, 144),
        "shadow": (92, 79, 63),
        "subject_glow": (255, 252, 247),
    },
    {
        "bg_top": (242, 247, 251),
        "bg_bottom": (228, 236, 244),
        "panel_fill": (249, 252, 255),
        "panel_edge": (182, 198, 214),
        "accent": (121, 164, 184),
        "accent_soft": (211, 231, 240),
        "decoy_tint": (116, 129, 146),
        "noise_ink": (164, 175, 189),
        "token_ink": (123, 134, 146),
        "shadow": (73, 93, 108),
        "subject_glow": (247, 252, 255),
    },
    {
        "bg_top": (248, 245, 237),
        "bg_bottom": (233, 227, 215),
        "panel_fill": (255, 250, 242),
        "panel_edge": (202, 188, 165),
        "accent": (173, 145, 111),
        "accent_soft": (236, 221, 198),
        "decoy_tint": (129, 122, 117),
        "noise_ink": (180, 170, 160),
        "token_ink": (139, 130, 123),
        "shadow": (96, 83, 68),
        "subject_glow": (255, 248, 238),
    },
]

# ── 氨基酸侧链（多肽生成用） ──
AMINO_ACID_SIDECHAINS = [
    "C", "CC(C)C", "CC(C)CC", "CC", "CCC",
    "CO", "CS", "CC(=O)O", "CC(=O)N",
    "CCCC(=O)O", "CCCC(=O)N", "CCCCN",
    "c1ccccc1C", "c1ccc(O)cc1C",
    "Cc1c[nH]cn1",                   # 组氨酸
    "Cc1c[nH]c2ccccc12",             # 色氨酸
    "CSCC",                          # 蛋氨酸
    "CC(C)O",                        # 苏氨酸变体
    "CCCNC(=N)N",                    # 精氨酸
]

# ── 复杂手性分子（药物/天然产物/生物碱/萜类，多手性中心） ──
COMPLEX_CHIRAL_SMILES = [
    # 糖类
    "OC(=O)[C@@H](O)[C@H](O)[C@@H](O)[C@H](O)CO",           # 葡萄糖酸
    "OC[C@H]1OC(O)[C@@H](O)[C@@H](O)[C@@H]1O",              # D-葡萄糖
    "OC[C@H]1OC(O)[C@H](O)[C@@H](O)[C@@H]1O",               # D-半乳糖
    "[C@@H]1(O)[C@H](O)[C@@H](O)[C@H](O)[C@@H](CO)O1",      # 吡喃糖
    "C[C@H](O)[C@@H](O)[C@H](O)[C@@H](O)CO",                # 甘露醇
    "OC[C@@H]1OC(O)[C@@H](O)[C@@H]1O",                       # D-核糖
    "OC[C@H]1OC(O)[C@@H](O)[C@@H](O)[C@H]1O",               # D-甘露糖
    # 甾体
    "CC(=O)O[C@@H]1CC[C@]2(C)[C@H]3CC[C@@]4(C)[C@@H](CC[C@@H]4[C@H]3CC=C2C1)C(C)=O",  # 孕酮乙酸酯
    "CC(=O)OCC(=O)[C@@]1(O)CC[C@@H]2[C@H]3CCC4=CC(=O)CC[C@]4(C)[C@@H]3[C@H](O)C[C@@]21C",  # 皮质醇乙酸酯
    "C[C@H](CCCC(C)C)[C@H]1CC[C@@H]2[C@]1(C)CC[C@H]1[C@@H]3CC=C4C[C@@H](O)CC[C@]4(C)[C@@H]3CC[C@@]21C",  # 胆固醇
    "O=C1CC[C@@]2(C)[C@H]3CC[C@@]4(C)[C@@H](O)CC=C4[C@H]3CC[C@H]2[C@@H]1O",  # 睾酮变体
    # 天然产物 / 药物
    "CC(C)C[C@@H](NC(=O)[C@H](CC(=O)O)NC(=O)[C@@H](N)Cc1ccccc1)C(=O)O",  # 三肽
    "O=C1C[C@H](c2ccc(O)cc2)[C@@H](c2ccc(O)cc2)O1",          # 黄酮醇内酯
    "CC1=CC(=O)[C@H](CC=C(C)C)[C@@H](O)C1",                  # 萜类酮
    "O[C@@H]1[C@@H](O)[C@H](O)[C@@H](O)[C@H](O)[C@H]1O",   # 肌醇
    "CC(=O)N[C@@H]1[C@@H](O)[C@H](O)[C@@H](CO)O[C@H]1O",   # N-乙酰氨基葡萄糖
    # 生物碱
    "CN1CC[C@]23c4c5ccc(O)c4O[C@H]2[C@@H](O)C=C[C@H]3[C@@H]1C5",  # 吗啡
    "COc1ccc2c(c1OC)-c1c(O)c(OC)cc3c1[C@@H](C2)[N@@+](C)CC3",  # 小檗碱变体
    "CC[C@@]1(O)C(=O)OCc2c1cc1n(c2=O)Cc2cc3ccccc3nc2-1",     # 喜树碱
    # 大环内酯
    "C[C@@H]1CC(=O)[C@H](C)[C@@H](O)[C@H](C)C[C@@H](C)C[C@H](OC(=O)C(C)C)[C@@H](C)[C@H](O)C1",  # 大环内酯
    # 核苷
    "Nc1ccn([C@@H]2O[C@H](CO)[C@@H](O)[C@H]2O)c(=O)n1",     # 胞苷
    "Nc1ncnc2c1ncn2[C@@H]1O[C@H](CO)[C@@H](O)[C@H]1O",      # 腺苷
    "O=c1ccn([C@@H]2O[C@H](CO)[C@@H](O)[C@H]2O)c(=O)[nH]1", # 尿苷
    "Cc1cn([C@@H]2O[C@H](CO)[C@@H](O)C2)c(=O)[nH]c1=O",     # 胸苷
    # 萜类
    "CC1=CC[C@@H](C(C)C)[C@@]2(C)CC[C@@H](C)[C@H](C)C12",   # 愈创木薁烷
    "C=C[C@H]1CN2CC[C@@H]1C[C@H]2[C@@H](O)c1ccnc2ccc(OC)cc12",  # 奎宁
    "CC(=O)O[C@H]1C[C@@H](OC(C)=O)[C@@]2(C)C(=O)C(OC(=O)c3ccccc3)=C3[C@@H](O)[C@@H]4[C@H](OC(=O)[C@@H](O)[C@@H]4C)[C@](O)(C3C(C)=O)[C@@H]2[C@H]1OC(=O)C",  # 紫杉醇简化
]

# ── 中等复杂度手性分子（氨基酸/简单糖/药物中间体） ──
MEDIUM_CHIRAL_SMILES = [
    # 氨基酸（全部 20 种天然氨基酸）
    "N[C@@H](C)C(=O)O",                   # 丙氨酸
    "N[C@@H](CC(=O)O)C(=O)O",             # 天冬氨酸
    "N[C@@H](CCC(=O)O)C(=O)O",            # 谷氨酸
    "N[C@@H](Cc1ccccc1)C(=O)O",           # 苯丙氨酸
    "N[C@@H](Cc1ccc(O)cc1)C(=O)O",        # 酪氨酸
    "N[C@@H](CS)C(=O)O",                  # 半胱氨酸
    "N[C@@H](CO)C(=O)O",                  # 丝氨酸
    "N[C@@H]([C@@H](C)O)C(=O)O",          # 苏氨酸
    "N[C@@H](CCCCN)C(=O)O",               # 赖氨酸
    "N[C@@H](CCCNC(=N)N)C(=O)O",          # 精氨酸
    "N[C@@H](Cc1c[nH]cn1)C(=O)O",         # 组氨酸
    "N[C@@H](Cc1c[nH]c2ccccc12)C(=O)O",   # 色氨酸
    "N[C@@H](CC(=O)N)C(=O)O",             # 天冬酰胺
    "N[C@@H](CCC(=O)N)C(=O)O",            # 谷氨酰胺
    "N[C@@H](CSCC)C(=O)O",                # 蛋氨酸（含S）
    "N[C@@H](CC(C)C)C(=O)O",              # 亮氨酸
    "N[C@@H]([C@@H](CC)C)C(=O)O",         # 异亮氨酸
    "N1CCC[C@@H]1C(=O)O",                 # 脯氨酸
    # 简单手性醇/卤代
    "CC(O)CC", "CC(Br)CC", "CC(F)(Cl)Br",
    "C[C@H](O)c1ccccc1",                  # 苯甲醇
    "CC(O)C(=O)O",                         # 乳酸
    "OCC(O)C=O",                           # 甘油醛
    "C[C@H](Cl)C(=O)O",                   # 氯丙酸
    "C[C@H](NH2)c1ccccc1",                # 苯乙胺
    # 简单糖/糖醇
    "OC[C@@H](O)[C@H](O)[C@@H](O)C=O",   # D-木糖
    "C[C@@H](O)[C@H](NC(C)=O)C(=O)O",    # N-乙酰苏氨酸
    "OC[C@@H](O)[C@@H](O)C(=O)O",         # 酒石酸变体
    # 药物中间体
    "C[C@@H](O)CC(=O)O",                  # 3-羟基丁酸
    "OC(=O)[C@@H](O)Cc1ccccc1",           # 扁桃酸
    "CC(=O)O[C@H](C)c1ccccc1",            # 乙酸苯乙酯
    "N[C@@H](CSc1ccc(O)cc1)C(=O)O",       # S-对羟苯基半胱氨酸
    "O=C(O)[C@H]1CCCN1",                  # 脯氨醇变体
    "C[C@H](NC(=O)C)[C@H](O)c1ccccc1",   # 麻黄碱衍生物
    "OC(=O)C[C@H](O)C[C@@H](O)CC(=O)O",  # 柠檬酸变体
]

# ── 干扰分子（无目标坐标，仅用于视觉干扰） ──
DECOY_FRAGMENTS = [
    "c1ccccc1", "C1CCCCC1", "C1CCNCC1", "c1ccncc1",
    "C(=O)O", "C(=O)N", "S(=O)(=O)O", "P(=O)(O)O",
]

NOISE_TOKENS = ["sp3", "R", "S", "E", "Z", "cis", "trans", "C*", "@@", "n"]

AUDIO_LANGUAGE_PROFILES = {
    "zh-cn": {
        "digit_words": {
            "0": "零", "1": "一", "2": "二", "3": "三", "4": "四",
            "5": "五", "6": "六", "7": "七", "8": "八", "9": "九",
        },
        "separator": "，",
        "voices": [
            "zh-CN-XiaoxiaoNeural",
            "zh-CN-YunxiNeural",
            "zh-CN-XiaoyiNeural",
        ],
        "rate": "-28%",
        "pitch": "+0Hz",
        "volume": "+0%",
    },
    "zh-tw": {
        "digit_words": {
            "0": "零", "1": "一", "2": "二", "3": "三", "4": "四",
            "5": "五", "6": "六", "7": "七", "8": "八", "9": "九",
        },
        "separator": "，",
        "voices": [
            "zh-TW-HsiaoChenNeural",
            "zh-TW-YunJheNeural",
            "zh-TW-HsiaoYuNeural",
        ],
        "rate": "-28%",
        "pitch": "+0Hz",
        "volume": "+0%",
    },
    "zh-hk": {
        "digit_words": {
            "0": "零", "1": "一", "2": "二", "3": "三", "4": "四",
            "5": "五", "6": "六", "7": "七", "8": "八", "9": "九",
        },
        "separator": "，",
        "voices": [
            "zh-HK-HiuGaaiNeural",
            "zh-HK-HiuMaanNeural",
            "zh-HK-WanLungNeural",
        ],
        "rate": "-28%",
        "pitch": "+0Hz",
        "volume": "+0%",
    },
    "en-us": {
        "digit_words": {
            "0": "zero", "1": "one", "2": "two", "3": "three", "4": "four",
            "5": "five", "6": "six", "7": "seven", "8": "eight", "9": "nine",
        },
        "separator": ", ",
        "voices": [
            "en-US-JennyNeural",
            "en-US-GuyNeural",
            "en-US-AriaNeural",
        ],
        "rate": "-22%",
        "pitch": "+0Hz",
        "volume": "+0%",
    },
    "en-gb": {
        "digit_words": {
            "0": "zero", "1": "one", "2": "two", "3": "three", "4": "four",
            "5": "five", "6": "six", "7": "seven", "8": "eight", "9": "nine",
        },
        "separator": ", ",
        "voices": [
            "en-GB-SoniaNeural",
            "en-GB-RyanNeural",
            "en-US-JennyNeural",
        ],
        "rate": "-22%",
        "pitch": "+0Hz",
        "volume": "+0%",
    },
    "ja-jp": {
        "digit_words": {
            "0": "ゼロ", "1": "いち", "2": "に", "3": "さん", "4": "よん",
            "5": "ご", "6": "ろく", "7": "なな", "8": "はち", "9": "きゅう",
        },
        "separator": "、",
        "voices": [
            "ja-JP-NanamiNeural",
            "ja-JP-KeitaNeural",
            "ja-JP-ShioriNeural",
        ],
        "rate": "-24%",
        "pitch": "+0Hz",
        "volume": "+0%",
    },
    "ko-kr": {
        "digit_words": {
            "0": "공", "1": "일", "2": "이", "3": "삼", "4": "사",
            "5": "오", "6": "육", "7": "칠", "8": "팔", "9": "구",
        },
        "separator": ", ",
        "voices": [
            "ko-KR-SunHiNeural",
            "ko-KR-InJoonNeural",
            "ko-KR-HyunsuNeural",
        ],
        "rate": "-24%",
        "pitch": "+0Hz",
        "volume": "+0%",
    },
}

AUDIO_LANGUAGE_ALIASES = {
    "zh": "zh-cn",
    "zh-cn": "zh-cn",
    "zh_cn": "zh-cn",
    "zh-hans": "zh-cn",
    "zh-sg": "zh-cn",
    "cn": "zh-cn",
    "zh-tw": "zh-tw",
    "zh_tw": "zh-tw",
    "zh-hant": "zh-tw",
    "tw": "zh-tw",
    "zh-hk": "zh-hk",
    "zh_hk": "zh-hk",
    "hk": "zh-hk",
    "en": "en-us",
    "en-us": "en-us",
    "en_us": "en-us",
    "us": "en-us",
    "english": "en-us",
    "en-gb": "en-gb",
    "en_gb": "en-gb",
    "gb": "en-gb",
    "uk": "en-gb",
    "ja": "ja-jp",
    "ja-jp": "ja-jp",
    "ja_jp": "ja-jp",
    "jp": "ja-jp",
    "japanese": "ja-jp",
    "ko": "ko-kr",
    "ko-kr": "ko-kr",
    "ko_kr": "ko-kr",
    "kr": "ko-kr",
    "korean": "ko-kr",
}


def _normalize_audio_lang(raw_lang):
    lang = str(raw_lang or "zh").strip().lower()
    if not lang:
        return "zh-cn"
    lang = lang.replace("_", "-")
    if lang in AUDIO_LANGUAGE_ALIASES:
        return AUDIO_LANGUAGE_ALIASES[lang]
    primary = lang.split("-", 1)[0]
    if primary in AUDIO_LANGUAGE_ALIASES:
        return AUDIO_LANGUAGE_ALIASES[primary]
    return "zh-cn"


def _resolve_audio_profile(raw_lang):
    lang = _normalize_audio_lang(raw_lang)
    return lang, AUDIO_LANGUAGE_PROFILES[lang]


def _build_spoken_digits(digits, profile):
    words = profile["digit_words"]
    return profile["separator"].join(words.get(d, d) for d in digits)


def _audio_voice_candidates(profile, requested_voice=""):
    candidates = []
    requested_voice = str(requested_voice or "").strip()
    if requested_voice:
        candidates.append(requested_voice)
    for voice in profile["voices"]:
        if voice not in candidates:
            candidates.append(voice)
    return candidates


async def _synthesize_audio_bytes(spoken_text, profile, requested_voice=""):
    import edge_tts

    last_error = None
    for voice in _audio_voice_candidates(profile, requested_voice):
        try:
            communicator = edge_tts.Communicate(
                spoken_text,
                voice=voice,
                rate=profile["rate"],
                volume=profile["volume"],
                pitch=profile["pitch"],
                connect_timeout=8,
                receive_timeout=45,
                boundary="WordBoundary",
            )
            chunks = []
            async for chunk in communicator.stream():
                if chunk["type"] == "audio":
                    chunks.append(chunk["data"])
            if chunks:
                return b"".join(chunks), voice
            last_error = RuntimeError(f"voice {voice} returned no audio data")
        except Exception as exc:
            last_error = exc

    if last_error is None:
        last_error = RuntimeError("no available voice candidates")
    raise last_error


def generate_peptide(length=None):
    if length is None:
        length = random.randint(4, 8)
    parts = []
    for i in range(length):
        sc = random.choice(AMINO_ACID_SIDECHAINS)
        if i == length - 1:
            parts.append(f"N[C@@H]({sc})C(=O)O")
        else:
            parts.append(f"N[C@@H]({sc})C(=O)")
    mol = Chem.MolFromSmiles("".join(parts), sanitize=False)
    if mol is None:
        return None
    try:
        Chem.SanitizeMol(mol)
    except Exception:
        return None
    return mol


def generate_molecule():
    """
    生成高难度分子（每次随机选取，极少重复）：
    40% 复杂天然产物/药物/生物碱（3~10+ 手性中心）
    25% 多肽（3~10 残基，每个残基一个手性中心）
    20% 中等手性分子（1~3 手性中心）
    15% 组合策略：随机拼接 2 个中等分子的 SMILES 片段
    """
    roll = random.random()

    if roll < 0.40:
        candidates = random.sample(COMPLEX_CHIRAL_SMILES, min(6, len(COMPLEX_CHIRAL_SMILES)))
        for smi in candidates:
            mol = Chem.MolFromSmiles(smi)
            if mol and Chem.FindMolChiralCenters(mol, includeUnassigned=True):
                return mol

    if roll < 0.65:
        length = random.choice([3, 4, 5, 5, 6, 6, 7, 8, 9, 10])
        mol = generate_peptide(length)
        if mol is not None:
            return mol

    if roll < 0.85:
        candidates = random.sample(MEDIUM_CHIRAL_SMILES, min(8, len(MEDIUM_CHIRAL_SMILES)))
        for smi in candidates:
            mol = Chem.MolFromSmiles(smi)
            if mol and Chem.FindMolChiralCenters(mol, includeUnassigned=True):
                return mol

    # 组合策略：拼两个中等分子生成新结构
    pair = random.sample(MEDIUM_CHIRAL_SMILES, 2)
    combined = f"{pair[0]}.{pair[1]}"
    mol = Chem.MolFromSmiles(combined)
    if mol and Chem.FindMolChiralCenters(mol, includeUnassigned=True):
        return mol

    # 最终回退
    fallback = random.choice(COMPLEX_CHIRAL_SMILES + MEDIUM_CHIRAL_SMILES)
    mol = Chem.MolFromSmiles(fallback)
    if mol:
        return mol
    return Chem.MolFromSmiles("N[C@@H](Cc1ccccc1)C(=O)O")


def find_chiral_carbons(mol):
    centers = Chem.FindMolChiralCenters(mol, includeUnassigned=True)
    indices = [idx for idx, _ in centers]
    if not indices:
        for atom in mol.GetAtoms():
            if atom.GetAtomicNum() != 6 or atom.GetDegree() != 4:
                continue
            sigs = set()
            for n in atom.GetNeighbors():
                sigs.add((n.GetAtomicNum(), n.GetDegree(), n.GetTotalNumHs()))
            if len(sigs) == 4:
                indices.append(atom.GetIdx())
    return indices


def _logical_canvas(width, height):
    base_w = 640
    base_h = max(420, round(base_w * height / width))
    return base_w, base_h


def _build_render_profile(width, height, mol):
    heavy_atoms = sum(1 for atom in mol.GetAtoms() if atom.GetAtomicNum() > 1)
    base_content_x = 0.90 if heavy_atoms <= 18 else 0.88
    base_content_y = 0.84 if heavy_atoms <= 18 else 0.80
    theme = random.choice(RENDER_THEMES)
    return {
        "theme": theme,
        "rotation_deg": random.uniform(-6.0, 6.0),
        "padding": random.uniform(0.014, 0.028),
        "font_scale": random.uniform(1.14, 1.34),
        "bond_width_boost": random.uniform(-0.12, 0.18),
        "bond_scale": 1.0 + random.uniform(-0.05, 0.06),
        "content_x": base_content_x + random.uniform(-0.01, 0.015),
        "content_y": base_content_y + random.uniform(-0.01, 0.015),
        "decoy_count": random.randint(2, 3),
        "noise_dots": random.randint(60, 110),
        "curve_count": random.randint(1, 3),
        "scan_step": random.randint(30, 42),
        "token_count": random.randint(3, 6),
        "star_true_ratio": random.uniform(0.55, 0.9),
        "panel_margin_x": random.uniform(0.082, 0.108),
        "panel_margin_y": random.uniform(0.102, 0.132),
        "panel_radius_ratio": random.uniform(0.034, 0.046),
        "spotlight_scale": random.uniform(0.56, 0.68),
        "subject_shadow_alpha": random.randint(44, 66),
        "subject_shadow_blur": random.uniform(8.0, 13.0),
        "subject_shadow_offset": random.randint(5, 9),
    }


def _mix_rgb(left, right, t):
    return tuple(
        int(round(left[i] * (1.0 - t) + right[i] * t))
        for i in range(3)
    )


def _rgba(color, alpha):
    return color + (alpha,)


def _extract_foreground(img):
    rgb = img.convert("RGB")
    diff = ImageChops.difference(rgb, Image.new("RGB", rgb.size, "white"))
    alpha = diff.convert("L").point(lambda p: 0 if p < 6 else min(255, int(p * 2.7)))
    alpha = alpha.filter(ImageFilter.GaussianBlur(0.4))
    foreground = rgb.convert("RGBA")
    foreground.putalpha(alpha)
    return foreground


def _focus_center(width, height, positions):
    if not positions:
        return width / 2, height / 2

    xs = [pos[0] * width for pos in positions.values()]
    ys = [pos[1] * height for pos in positions.values()]
    return sum(xs) / len(xs), sum(ys) / len(ys)


def _create_backdrop(width, height, positions, profile):
    theme = profile["theme"]
    canvas = Image.new("RGBA", (width, height), _rgba(theme["bg_top"], 255))
    draw = ImageDraw.Draw(canvas, "RGBA")

    for y in range(height):
        color = _mix_rgb(theme["bg_top"], theme["bg_bottom"], y / max(height - 1, 1))
        draw.line((0, y, width, y), fill=_rgba(color, 255))

    focus_x, focus_y = _focus_center(width, height, positions)
    glow = Image.new("RGBA", (width, height), (0, 0, 0, 0))
    glow_draw = ImageDraw.Draw(glow, "RGBA")
    rx = int(width * profile["spotlight_scale"])
    ry = int(height * profile["spotlight_scale"] * 0.72)
    glow_draw.ellipse(
        (focus_x - rx, focus_y - ry, focus_x + rx, focus_y + ry),
        fill=_rgba(theme["accent_soft"], 120),
    )
    glow_draw.ellipse(
        (focus_x - int(rx * 0.72), focus_y - int(ry * 0.68), focus_x + int(rx * 0.72), focus_y + int(ry * 0.68)),
        fill=_rgba(theme["subject_glow"], 82),
    )
    glow = glow.filter(ImageFilter.GaussianBlur(max(20, min(width, height) // 22)))
    canvas = Image.alpha_composite(canvas, glow)

    margin_x = int(width * profile["panel_margin_x"])
    margin_y = int(height * profile["panel_margin_y"])
    panel_box = (margin_x, margin_y, width - margin_x, height - margin_y)
    radius = int(min(width, height) * profile["panel_radius_ratio"])

    shadow = Image.new("RGBA", (width, height), (0, 0, 0, 0))
    shadow_draw = ImageDraw.Draw(shadow, "RGBA")
    offset = profile["subject_shadow_offset"] + 5
    shadow_box = (
        panel_box[0],
        min(height - 1, panel_box[1] + offset),
        panel_box[2],
        min(height - 1, panel_box[3] + offset),
    )
    shadow_draw.rounded_rectangle(
        shadow_box,
        radius=radius,
        fill=_rgba(theme["shadow"], 24),
    )
    shadow = shadow.filter(ImageFilter.GaussianBlur(max(14, min(width, height) // 26)))
    canvas = Image.alpha_composite(canvas, shadow)

    panel = Image.new("RGBA", (width, height), (0, 0, 0, 0))
    panel_draw = ImageDraw.Draw(panel, "RGBA")
    panel_draw.rounded_rectangle(
        panel_box,
        radius=radius,
        fill=_rgba(theme["panel_fill"], 230),
        outline=_rgba(theme["panel_edge"], 110),
        width=max(1, min(width, height) // 320),
    )

    inset = max(16, min(width, height) // 36)
    panel_draw.line(
        (panel_box[0] + inset, panel_box[1] + inset, panel_box[2] - inset, panel_box[1] + inset),
        fill=_rgba((255, 255, 255), 82),
        width=1,
    )
    panel_draw.line(
        (panel_box[0] + inset, panel_box[3] - inset, panel_box[2] - inset, panel_box[3] - inset),
        fill=_rgba(theme["panel_edge"], 22),
        width=1,
    )

    accent_pad = max(22, min(width, height) // 28)
    for corner_x in (panel_box[0] + accent_pad, panel_box[2] - accent_pad):
        for corner_y in (panel_box[1] + accent_pad, panel_box[3] - accent_pad):
            panel_draw.ellipse(
                (corner_x - 3, corner_y - 3, corner_x + 3, corner_y + 3),
                fill=_rgba(theme["accent"], 72),
            )

    canvas = Image.alpha_composite(canvas, panel)
    return canvas


def _add_subject_layer(base, subject, profile):
    alpha = subject.getchannel("A")
    if alpha.getbbox() is None:
        return base

    theme = profile["theme"]
    glow_alpha = alpha.filter(ImageFilter.GaussianBlur(max(10, min(base.width, base.height) // 42)))
    glow = Image.new("RGBA", base.size, _rgba(theme["subject_glow"], 0))
    glow.putalpha(glow_alpha.point(lambda p: int(p * 0.16)))

    shadow_alpha = alpha.filter(ImageFilter.GaussianBlur(profile["subject_shadow_blur"]))
    shadow = Image.new("RGBA", base.size, _rgba(theme["shadow"], 0))
    shadow.putalpha(shadow_alpha.point(lambda p: int(p * profile["subject_shadow_alpha"] / 255)))
    shifted_shadow = Image.new("RGBA", base.size, (0, 0, 0, 0))
    shifted_shadow.paste(shadow, (0, profile["subject_shadow_offset"]), shadow)

    composed = Image.alpha_composite(base, glow)
    composed = Image.alpha_composite(composed, shifted_shadow)
    composed = Image.alpha_composite(composed, subject)
    return composed


def _draw_options(mol, width, height, profile):
    opts = rdMolDraw2D.MolDrawOptions()
    heavy_atoms = sum(1 for atom in mol.GetAtoms() if atom.GetAtomicNum() > 1)
    canvas_min = min(width, height)

    opts.addStereoAnnotation = False
    opts.addAtomIndices = False
    opts.explicitMethyl = True
    opts.padding = profile["padding"]
    opts.additionalAtomLabelPadding = 0.08
    opts.annotationFontScale = profile["font_scale"]
    opts.minFontSize = max(20, canvas_min // 22)
    opts.maxFontSize = max(opts.minFontSize + 10, canvas_min // 10)
    base_bond_width = 2.55 if heavy_atoms <= 18 else 2.2
    opts.bondLineWidth = max(1.95, base_bond_width + profile["bond_width_boost"])
    opts.multipleBondOffset = 0.18
    opts.centreMoleculesBeforeDrawing = True
    opts.prepareMolsBeforeDrawing = True
    opts.clearBackground = True
    opts.setBackgroundColour((1.0, 1.0, 1.0, 1.0))
    opts.updateAtomPalette(ATOM_PALETTE)

    if heavy_atoms <= 10:
        base_bond_length = 58
    elif heavy_atoms <= 18:
        base_bond_length = 46
    elif heavy_atoms <= 28:
        base_bond_length = 36
    else:
        base_bond_length = 30
    opts.fixedBondLength = max(24, int(round(base_bond_length * profile["bond_scale"])))
    return opts


def _replace_svg_styles(svg_text):
    return svg_text.replace(
        "font-family:sans-serif",
        "font-family:'Segoe UI','Helvetica Neue',Helvetica,Arial,sans-serif"
    )


def _pick_starred_atoms(mol, chiral_indices, profile):
    true_candidates = [
        idx for idx in chiral_indices
        if idx < mol.GetNumAtoms() and mol.GetAtomWithIdx(idx).GetAtomicNum() == 6
    ]
    false_candidates = []
    for atom in mol.GetAtoms():
        idx = atom.GetIdx()
        if idx in chiral_indices:
            continue
        if atom.GetAtomicNum() != 6:
            continue
        if atom.GetDegree() < 2:
            continue
        false_candidates.append(idx)

    starred = set()
    if true_candidates:
        true_count = max(1, int(round(len(true_candidates) * profile["star_true_ratio"])))
        true_count = min(true_count, len(true_candidates))
        starred.update(random.sample(true_candidates, true_count))

    if false_candidates:
        min_false = 1 if true_candidates else min(2, len(false_candidates))
        max_false = min(len(false_candidates), max(min_false, len(starred) + 1))
        if max_false >= min_false:
            false_count = random.randint(min_false, max_false)
            starred.update(random.sample(false_candidates, false_count))

    return sorted(starred)


def _trim_and_fit(img, width, height, positions, profile):
    rgb = img.convert("RGB")
    diff = ImageChops.difference(rgb, Image.new("RGB", rgb.size, "white"))
    bbox = diff.getbbox()
    if not bbox:
        fallback = _extract_foreground(img.resize((width, height), RESAMPLING))
        normalized = {
            idx: (pos[0] / img.width, pos[1] / img.height)
            for idx, pos in positions.items()
        }
        return fallback, normalized

    left, top, right, bottom = bbox
    draw_w = right - left
    draw_h = bottom - top

    margin_x = max(int(draw_w * 0.14), 28)
    margin_y = max(int(draw_h * 0.18), 28)
    left = max(0, left - margin_x)
    top = max(0, top - margin_y)
    right = min(img.width, right + margin_x)
    bottom = min(img.height, bottom + margin_y)

    cropped = _extract_foreground(img.crop((left, top, right, bottom)).convert("RGBA"))
    content_w = max(1, int(width * profile["content_x"]))
    content_h = max(1, int(height * profile["content_y"]))
    scale = min(content_w / cropped.width, content_h / cropped.height)

    scaled_w = max(1, int(round(cropped.width * scale)))
    scaled_h = max(1, int(round(cropped.height * scale)))
    resized = cropped.resize((scaled_w, scaled_h), RESAMPLING)

    canvas = Image.new("RGBA", (width, height), (255, 255, 255, 0))
    offset_x = (width - scaled_w) // 2
    offset_y = (height - scaled_h) // 2
    canvas.alpha_composite(resized, (offset_x, offset_y))

    transformed = {}
    for idx, (x, y) in positions.items():
        px = offset_x + (x - left) * scale
        py = offset_y + (y - top) * scale
        transformed[idx] = (
            round(min(max(px / width, 0.0), 1.0), 6),
            round(min(max(py / height, 0.0), 1.0), 6),
        )

    return canvas, transformed


def _rotate_positions(positions, width, height, angle_deg):
    if abs(angle_deg) < 0.01:
        return positions

    angle = math.radians(angle_deg)
    cos_a = math.cos(angle)
    sin_a = math.sin(angle)
    cx = width / 2
    cy = height / 2
    rotated = {}

    for idx, (nx, ny) in positions.items():
        x = nx * width - cx
        y = ny * height - cy
        rx = x * cos_a + y * sin_a + cx
        ry = -x * sin_a + y * cos_a + cy
        rotated[idx] = (
            round(min(max(rx / width, 0.0), 1.0), 6),
            round(min(max(ry / height, 0.0), 1.0), 6),
        )

    return rotated


def _rotate_scene(img, positions, angle_deg):
    if abs(angle_deg) < 0.01:
        return img, positions
    rotated = img.rotate(
        angle_deg,
        resample=Image.Resampling.BICUBIC,
        expand=False,
        fillcolor=(255, 255, 255, 0),
    )
    return rotated, _rotate_positions(positions, img.width, img.height, angle_deg)


def _render_decoy_fragment(smiles, target_w, target_h, tint_color):
    mol = Chem.MolFromSmiles(smiles)
    if mol is None:
        return None

    AllChem.Compute2DCoords(mol, clearConfs=True)
    drawer = rdMolDraw2D.MolDraw2DSVG(240, 180)
    opts = rdMolDraw2D.MolDrawOptions()
    opts.addStereoAnnotation = False
    opts.addAtomIndices = False
    opts.explicitMethyl = True
    opts.padding = 0.05
    opts.minFontSize = 12
    opts.maxFontSize = 20
    opts.annotationFontScale = 1.0
    opts.bondLineWidth = 1.9
    opts.fixedBondLength = 30
    opts.centreMoleculesBeforeDrawing = True
    opts.prepareMolsBeforeDrawing = True
    opts.clearBackground = True
    opts.setBackgroundColour((1.0, 1.0, 1.0, 1.0))
    drawer.SetDrawOptions(opts)

    rdMolDraw2D.PrepareAndDrawMolecule(drawer, mol)
    drawer.FinishDrawing()
    svg_text = _replace_svg_styles(drawer.GetDrawingText())
    png_raw = cairosvg.svg2png(
        bytestring=svg_text.encode("utf-8"),
        output_width=target_w * 2,
        output_height=target_h * 2,
    )
    img = Image.open(io.BytesIO(png_raw)).convert("RGBA")
    alpha = _extract_foreground(img).getchannel("A").point(lambda p: 0 if p < 10 else min(255, int(p * 1.6)))
    bbox = alpha.getbbox()
    if not bbox:
        return None

    alpha = alpha.crop(bbox)
    tint = Image.new("RGBA", alpha.size, _rgba(tint_color, 0))
    tint.putalpha(alpha)
    return tint


def _add_decoy_fragments(img, width, height, profile):
    canvas = img.copy()
    tint = profile["theme"]["decoy_tint"]
    anchors = [
        (0.12, 0.14), (0.88, 0.15),
        (0.11, 0.50), (0.89, 0.50),
        (0.13, 0.84), (0.87, 0.83),
    ]
    random.shuffle(anchors)
    decoy_smiles = random.sample(DECOY_FRAGMENTS, k=min(profile["decoy_count"], len(DECOY_FRAGMENTS)))

    for anchor, smiles in zip(anchors[:len(decoy_smiles)], decoy_smiles):
        decoy = _render_decoy_fragment(smiles, 220, 170, tint)
        if decoy is None:
            continue

        if random.random() < 0.5:
            decoy = ImageOps.mirror(decoy)

        scale = random.uniform(0.72, 1.10)
        w = max(1, int(decoy.width * scale))
        h = max(1, int(decoy.height * scale))
        decoy = decoy.resize((w, h), RESAMPLING)
        decoy = decoy.rotate(
            random.uniform(-20.0, 20.0),
            resample=Image.Resampling.BICUBIC,
            expand=True,
            fillcolor=(255, 255, 255, 0),
        )
        opacity_scale = random.uniform(0.16, 0.28)
        decoy.putalpha(decoy.getchannel("A").point(lambda p: int(p * opacity_scale)))

        x = int(anchor[0] * width - decoy.width / 2)
        y = int(anchor[1] * height - decoy.height / 2)
        x = max(0, min(width - decoy.width, x))
        y = max(0, min(height - decoy.height, y))
        canvas.alpha_composite(decoy, (x, y))

    return canvas


def _far_from_targets(px, py, positions, width, height, radius):
    for x, y in positions.values():
        dx = px - x * width
        dy = py - y * height
        if dx * dx + dy * dy <= radius * radius:
            return False
    return True


def _add_noise_overlay(img, positions, profile):
    width, height = img.size
    theme = profile["theme"]
    overlay = Image.new("RGBA", (width, height), (255, 255, 255, 0))
    draw = ImageDraw.Draw(overlay, "RGBA")

    for y in range(random.randint(10, 18), height, profile["scan_step"]):
        draw.line((0, y, width, y), fill=_rgba(theme["noise_ink"], random.randint(8, 15)), width=1)

    for _ in range(profile["curve_count"]):
        points = []
        side = random.choice(("top", "bottom"))
        baseline = random.randint(48, 120) if side == "top" else height - random.randint(48, 120)
        amplitude = random.randint(8, 22)
        phase = random.uniform(0, math.pi * 2)
        step = max(24, width // 18)
        frequency = random.randint(2, 4)
        for x in range(0, width + step, step):
            y = baseline + amplitude * math.sin((x / max(width, 1)) * math.pi * frequency + phase)
            points.append((x, y))
        draw.line(points, fill=_rgba(theme["accent"], random.randint(16, 28)), width=1)

    dot_radius = 18
    for _ in range(profile["noise_dots"]):
        px = random.randint(0, width - 1)
        py = random.randint(0, height - 1)
        if not _far_from_targets(px, py, positions, width, height, dot_radius):
            continue
        r = random.randint(1, 3)
        alpha = random.randint(14, 34)
        draw.ellipse((px - r, py - r, px + r, py + r), fill=_rgba(theme["noise_ink"], alpha))

    edge_boxes = [
        (random.randint(22, 110), random.randint(22, 72)),
        (width - random.randint(180, 260), random.randint(22, 72)),
        (random.randint(22, 110), height - random.randint(78, 132)),
        (width - random.randint(180, 260), height - random.randint(78, 132)),
    ]
    random.shuffle(edge_boxes)
    for x, y in edge_boxes[:profile["token_count"]]:
        draw.text((x, y), random.choice(NOISE_TOKENS), fill=_rgba(theme["token_ink"], random.randint(42, 80)))

    return Image.alpha_composite(img, overlay)


def render(mol, width, height, chiral_indices):
    """
    更高难度渲染：
    1. 主分子高清绘制并自动裁边放大
    2. 小角度随机旋转
    3. 叠加边缘伪分子干扰和轻量噪声纹理
    4. 星号真假混合提示：部分正确、部分错误
    """
    mol_to_draw = Chem.Mol(mol)
    AllChem.Compute2DCoords(mol_to_draw, clearConfs=True)
    profile = _build_render_profile(width, height, mol_to_draw)
    star_indices = _pick_starred_atoms(mol_to_draw, chiral_indices, profile)

    for idx in star_indices:
        if idx < mol_to_draw.GetNumAtoms():
            mol_to_draw.GetAtomWithIdx(idx).SetProp("atomNote", "*")

    svg_w, svg_h = _logical_canvas(width, height)
    drawer = rdMolDraw2D.MolDraw2DSVG(svg_w, svg_h)
    drawer.SetDrawOptions(_draw_options(mol_to_draw, svg_w, svg_h, profile))

    rdMolDraw2D.PrepareAndDrawMolecule(drawer, mol_to_draw)
    drawer.FinishDrawing()
    svg_text = _replace_svg_styles(drawer.GetDrawingText())

    working_w = width * 2
    working_h = height * 2
    positions = {}
    for idx in chiral_indices:
        if idx < mol_to_draw.GetNumAtoms():
            pt = drawer.GetDrawCoords(idx)
            positions[idx] = (pt.x * working_w / svg_w, pt.y * working_h / svg_h)

    png_raw = cairosvg.svg2png(
        bytestring=svg_text.encode("utf-8"),
        output_width=working_w,
        output_height=working_h,
    )
    img = Image.open(io.BytesIO(png_raw)).convert("RGBA")
    subject_layer, normalized_positions = _trim_and_fit(img, width, height, positions, profile)
    subject_layer, normalized_positions = _rotate_scene(subject_layer, normalized_positions, profile["rotation_deg"])
    scene = _create_backdrop(width, height, normalized_positions, profile)
    scene = _add_subject_layer(scene, subject_layer, profile)
    scene = _add_decoy_fragments(scene, width, height, profile)
    scene = _add_noise_overlay(scene, normalized_positions, profile)
    scene = scene.filter(ImageFilter.UnsharpMask(radius=0.7, percent=84, threshold=2))

    buf = io.BytesIO()
    scene.save(buf, format="PNG", optimize=True)
    png = buf.getvalue()
    return png, normalized_positions


@app.route("/generate", methods=["POST", "GET"])
def generate():
    width = int(request.args.get("width", 1600))
    height = int(request.args.get("height", 1200))
    width = max(width, 1200)
    height = max(height, 900)
    try:
        mol = generate_molecule()
        if mol is None:
            return jsonify({"error": "molecule generation failed"}), 500
        chiral = find_chiral_carbons(mol)
        if not chiral:
            mol = Chem.MolFromSmiles("CC(Br)CC")
            chiral = find_chiral_carbons(mol)
        png, positions = render(mol, width, height, chiral)
        targets = [
            {"x": round(positions[i][0], 4), "y": round(positions[i][1], 4), "t": 0.07}
            for i in chiral if i in positions
        ]
        return jsonify({
            "image": base64.b64encode(png).decode("ascii"),
            "targets": targets,
            "hint": "点击主分子中的所有手性碳，星号不一定正确。",
        })
    except Exception:
        return jsonify({"error": traceback.format_exc()}), 500


@app.route("/generate-audio", methods=["POST", "GET"])
def generate_audio():
    """生成多语言数字语音验证码（edge-tts，多 voice 回退）"""
    raw_length = str(request.args.get("length", "6")).strip()
    raw_lang = request.args.get("lang", "zh")
    requested_voice = request.args.get("voice", "")

    try:
        length = int(raw_length)
    except ValueError:
        return jsonify({"error": "invalid length"}), 400

    length = min(max(length, 4), 8)
    try:
        import asyncio

        digits = [str(random.randint(0, 9)) for _ in range(length)]
        answer = "".join(digits)
        lang, profile = _resolve_audio_profile(raw_lang)
        spoken = _build_spoken_digits(digits, profile)

        async def _synthesize():
            return await _synthesize_audio_bytes(spoken, profile, requested_voice)

        loop = asyncio.new_event_loop()
        try:
            audio_bytes, voice = loop.run_until_complete(_synthesize())
        finally:
            loop.close()

        return jsonify({
            "audio": base64.b64encode(audio_bytes).decode("ascii"),
            "answer": answer,
            "lang": lang,
            "voice": voice,
            "mimeType": "audio/mpeg",
        })
    except Exception:
        return jsonify({"error": traceback.format_exc()}), 500


@app.route("/health", methods=["GET"])
def health():
    return jsonify({"status": "ok"})


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000)
