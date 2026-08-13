// 大地基准（datum）：WGS-84 ⇄ GCJ-02
//
// 平台里流动的坐标**只有一种**：GeoIP 解析出来的 WGS-84。
// 但中国大陆的地图服务（高德 / 腾讯 / 智图）依法只提供 GCJ-02 加密偏移后的
// 底图，同一个地点在两套基准下相差约 300–700 米。
//
// 因此这份转换只服务于「把 GCJ-02 底图搬回 WGS-84 位置」这一件事
// （见 gcj02-tiles.ts），业务数据自始至终不做转换 —— 一旦数据也跟着变，
// 就再没人说得清库里那个坐标到底是哪套基准了。
//
// 算法是公开的经验拟合式，精度约 ±1–2 米，远优于 GeoIP 本身的城市级误差。

/** 克拉索夫斯基椭球长半轴（GCJ-02 偏移公式使用的椭球） */
const SEMI_MAJOR_AXIS = 6378245.0;
/** 该椭球的第一偏心率平方 */
const ECCENTRICITY_SQ = 0.00669342162296594323;

/**
 * 粗略的中国大陆包围盒。
 *
 * GCJ-02 的偏移只在境内生效，境外两套基准完全重合。这个判据本身就是
 * 「标准」的一部分（各家实现用的都是同一个盒子），所以刻意不改成精确国界：
 * 换成精确边界反而会与底图供应商的实现对不上，在边境线上出现二次偏移。
 */
export function outOfChina(lng: number, lat: number): boolean {
  return lng < 72.004 || lng > 137.8347 || lat < 0.8293 || lat > 55.8271;
}

function transformLat(x: number, y: number): number {
  let ret = -100 + 2 * x + 3 * y + 0.2 * y * y + 0.1 * x * y + 0.2 * Math.sqrt(Math.abs(x));
  ret += ((20 * Math.sin(6 * x * Math.PI) + 20 * Math.sin(2 * x * Math.PI)) * 2) / 3;
  ret += ((20 * Math.sin(y * Math.PI) + 40 * Math.sin((y / 3) * Math.PI)) * 2) / 3;
  ret += ((160 * Math.sin((y / 12) * Math.PI) + 320 * Math.sin((y * Math.PI) / 30)) * 2) / 3;
  return ret;
}

function transformLng(x: number, y: number): number {
  let ret = 300 + x + 2 * y + 0.1 * x * x + 0.1 * x * y + 0.1 * Math.sqrt(Math.abs(x));
  ret += ((20 * Math.sin(6 * x * Math.PI) + 20 * Math.sin(2 * x * Math.PI)) * 2) / 3;
  ret += ((20 * Math.sin(x * Math.PI) + 40 * Math.sin((x / 3) * Math.PI)) * 2) / 3;
  ret += ((150 * Math.sin((x / 12) * Math.PI) + 300 * Math.sin((x / 30) * Math.PI)) * 2) / 3;
  return ret;
}

/** WGS-84 → GCJ-02；境外原样返回 */
export function wgs84ToGcj02(lng: number, lat: number): [number, number] {
  if (outOfChina(lng, lat)) return [lng, lat];

  let dLat = transformLat(lng - 105, lat - 35);
  let dLng = transformLng(lng - 105, lat - 35);

  const radLat = (lat / 180) * Math.PI;
  const sinLat = Math.sin(radLat);
  const magic = 1 - ECCENTRICITY_SQ * sinLat * sinLat;
  const sqrtMagic = Math.sqrt(magic);

  dLat = (dLat * 180) / (((SEMI_MAJOR_AXIS * (1 - ECCENTRICITY_SQ)) / (magic * sqrtMagic)) * Math.PI);
  dLng = (dLng * 180) / (((SEMI_MAJOR_AXIS / sqrtMagic) * Math.cos(radLat)) * Math.PI);

  return [lng + dLng, lat + dLat];
}

/**
 * GCJ-02 → WGS-84。
 *
 * 正变换没有解析逆，用不动点迭代逼近：三轮之后残差已在毫米量级，
 * 再迭代只是浪费 CPU。
 */
export function gcj02ToWgs84(lng: number, lat: number): [number, number] {
  if (outOfChina(lng, lat)) return [lng, lat];

  let wgsLng = lng;
  let wgsLat = lat;
  for (let i = 0; i < 3; i++) {
    const [gcjLng, gcjLat] = wgs84ToGcj02(wgsLng, wgsLat);
    wgsLng += lng - gcjLng;
    wgsLat += lat - gcjLat;
  }
  return [wgsLng, wgsLat];
}

/** 底图使用的大地基准 */
export type MapDatum = "wgs84" | "gcj02";
