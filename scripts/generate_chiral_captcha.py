#!/usr/bin/env python3
"""
手性碳验证码生成器 — 使用 RDKit 生成随机多肽/有机分子，
检测手性碳，渲染为 PNG，输出 JSON。

用法：python generate_chiral_captcha.py [width] [height]
输出：JSON { "image": "base64...", "targets": [{"x":0.5,"y":0.3,"t":0.06}], "hint": "..." }
"""

import base64
import io
import json
import random
import sys

from rdkit import Chem
from rdkit.Chem import AllChem, Draw
from rdkit.Chem.Draw import rdMolDraw2D


# ── 分子生成策略 ──

# 常见氨基酸（含手性碳）
AMINO_ACIDS = [
    "C", "CC(C)C", "CC(C)CC",  # Gly, Val, Leu (侧链)
    "CC", "CCC", "CCCC",  # Ala, Abu, Nva
    "CO", "CS", "CC(=O)O", "CC(=O)N",  # Ser, Cys, Asp, Asn
    "CCCC(=O)O", "CCCC(=O)N",  # Glu, Gln
    "CCCCN", "CCCNC(=N)N",  # Lys, Arg
    "c1ccccc1C", "c1ccc(O)cc1C",  # Phe, Tyr
    "c1c[nH]c2ccccc12C",  # Trp (简化)
]

def generate_random_peptide(length=None):
    """生成随机多肽分子（2-5 个氨基酸残基）"""
    if length is None:
        length = random.randint(2, 5)

    # 构建多肽 SMILES
    residues = []
    for _ in range(length):
        side_chain = random.choice(AMINO_ACIDS)
        residues.append(side_chain)

    # 手动构建多肽骨架
    smiles_parts = []
    for i, sc in enumerate(residues):
        if i == 0:
            smiles_parts.append(f"N[C@@H]({sc})C(=O)")
        elif i == length - 1:
            smiles_parts.append(f"N[C@@H]({sc})C(=O)O")
        else:
            smiles_parts.append(f"N[C@@H]({sc})C(=O)")

    smiles = "".join(smiles_parts)
    mol = Chem.MolFromSmiles(smiles, sanitize=False)
    if mol is None:
        return None
    try:
        Chem.SanitizeMol(mol)
    except:
        return None
    return mol


# 带手性碳的简单有机分子库
CHIRAL_SMILES = [
    "CC(O)CC",                          # 2-丁醇
    "CC(Br)CC",                         # 2-溴丁烷
    "CC(F)(Cl)Br",                      # 氟氯溴甲烷
    "CC(O)(CC)C(=O)O",                  # 2-羟基-2-甲基丁酸
    "OC(CC)C(=O)O",                     # 2-羟基丁酸
    "CC(N)C(=O)O",                      # 丙氨酸
    "CC(N)CC(=O)O",                     # 3-氨基丁酸
    "NC(CC(=O)O)C(=O)O",               # 天冬氨酸
    "NC(CCC(=O)O)C(=O)O",              # 谷氨酸
    "NC(Cc1ccccc1)C(=O)O",             # 苯丙氨酸
    "NC(Cc1ccc(O)cc1)C(=O)O",          # 酪氨酸
    "NC(CS)C(=O)O",                    # 半胱氨酸
    "NC(CO)C(=O)O",                    # 丝氨酸
    "NC(C(C)O)C(=O)O",                # 苏氨酸
    "NC(CCCCN)C(=O)O",                # 赖氨酸
    "NC(Cc1c[nH]c2ccccc12)C(=O)O",    # 色氨酸
    "CC(O)C(=O)O",                     # 乳酸
    "OCC(O)C=O",                       # 甘油醛
    "CC(Cl)C(=O)O",                    # 2-氯丙酸
    "CC(O)c1ccccc1",                   # 1-苯乙醇
]


def generate_random_molecule():
    """生成随机有机分子（优先多肽，兜底用简单分子）"""
    # 50% 概率尝试多肽
    if random.random() < 0.5:
        mol = generate_random_peptide()
        if mol is not None:
            return mol

    # 从预定义手性分子中选择
    random.shuffle(CHIRAL_SMILES)
    for smi in CHIRAL_SMILES:
        mol = Chem.MolFromSmiles(smi)
        if mol is not None:
            chiral = Chem.FindMolChiralCenters(mol, includeUnassigned=True)
            if chiral:
                return mol

    # 最终兜底
    return Chem.MolFromSmiles("CC(Br)CC")


def find_chiral_carbons(mol):
    """检测分子中的手性碳原子，返回原子索引列表"""
    # RDKit 内置手性中心检测
    chiral_centers = Chem.FindMolChiralCenters(mol, includeUnassigned=True)
    indices = [idx for idx, _ in chiral_centers]

    # 如果 RDKit 没检测到（未标注立体化学的情况），手动检查
    if not indices:
        for atom in mol.GetAtoms():
            if atom.GetAtomicNum() != 6:  # 只检查碳
                continue
            if atom.GetDegree() != 4:  # sp3 需要 4 个键
                continue
            # 检查 4 个邻居是否都不同
            neighbors = []
            for n in atom.GetNeighbors():
                # 简单签名：原子序号 + 度
                sig = (n.GetAtomicNum(), n.GetDegree(), n.GetTotalNumHs())
                neighbors.append(sig)
            if len(set(neighbors)) == len(neighbors):
                indices.append(atom.GetIdx())

    return indices


def render_molecule(mol, width, height, chiral_indices):
    """渲染分子为 PNG，返回 (png_bytes, atom_positions)"""
    # 生成 2D 坐标
    AllChem.Compute2DCoords(mol)

    # 高亮手性碳原子（用 * 标注）
    drawer = rdMolDraw2D.MolDraw2DCairo(width, height)
    opts = drawer.drawOptions()
    opts.addStereoAnnotation = True
    opts.addAtomIndices = False
    opts.bondLineWidth = 2.0
    opts.padding = 0.15

    # 绘制
    drawer.DrawMolecule(mol, highlightAtoms=chiral_indices,
                        highlightAtomColors={i: (0.9, 0.7, 0.7) for i in chiral_indices})
    drawer.FinishDrawing()
    png_data = drawer.GetDrawingText()

    # 获取原子坐标（像素坐标）
    conf = mol.GetConformer()
    atom_positions = {}

    # 使用 drawer 的坐标转换
    for idx in chiral_indices:
        point = drawer.GetDrawCoords(idx)
        atom_positions[idx] = (point.x / width, point.y / height)

    return png_data, atom_positions


def main():
    width = int(sys.argv[1]) if len(sys.argv) > 1 else 480
    height = int(sys.argv[2]) if len(sys.argv) > 2 else 360

    mol = generate_random_molecule()
    if mol is None:
        print(json.dumps({"error": "无法生成分子"}))
        sys.exit(1)

    # 添加氢原子用于渲染（但不影响手性检测）
    mol_with_h = Chem.AddHs(mol)
    AllChem.EmbedMolecule(mol_with_h, randomSeed=random.randint(0, 99999))

    # 检测手性碳（在去氢分子上）
    chiral_indices = find_chiral_carbons(mol)
    if not chiral_indices:
        # 强制使用已知手性分子
        mol = Chem.MolFromSmiles("CC(Br)CC")
        chiral_indices = find_chiral_carbons(mol)

    # 渲染（使用去氢分子，骨架式更清晰）
    png_data, atom_positions = render_molecule(mol, width, height, chiral_indices)

    # 构建目标
    targets = []
    for idx in chiral_indices:
        if idx in atom_positions:
            x, y = atom_positions[idx]
            targets.append({"x": round(x, 4), "y": round(y, 4), "t": 0.06})

    if not targets:
        print(json.dumps({"error": "无法定位手性碳坐标"}))
        sys.exit(1)

    # 输出 JSON
    result = {
        "image": base64.b64encode(png_data).decode("ascii"),
        "targets": targets,
        "hint": f"请点击分子结构中的手性碳原子（共 {len(targets)} 个，标记为粉色高亮区域）",
        "smiles": Chem.MolToSmiles(mol),
        "chiralCount": len(targets)
    }

    print(json.dumps(result))


if __name__ == "__main__":
    main()
