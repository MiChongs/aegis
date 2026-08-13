// 底图供应商目录与坐标基准的行为约束。
//
// 这两处判错都不会报错：
//   - 语言规则错了，只是中国大陆的使用者拿到一张加载不出来的境外底图；
//   - GCJ-02 转换的正负号错了，底图不是回到正确位置而是偏出双倍距离（约 1km），
//     而 GeoIP 本身就有城市级误差，看图的人只会以为「数据就是这么糙」。
// 因此每一条规则都必须有测试直接盯着 —— 与 forwarded-headers.test.mjs 同一个理由。
//
//   pnpm test
//
// 直接 import 两个 .ts 源文件：它们都不依赖打包器别名（同目录用相对路径、
// 且那一条还是 `import type`，类型擦除后运行时零依赖），node 的类型擦除即可运行。

import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { gcj02ToWgs84, outOfChina, wgs84ToGcj02 } from "../src/lib/geo/datum.ts";
import {
  MAP_PROVIDERS,
  detectMapLocale,
  effectiveLang,
  resolveProvider
} from "../src/lib/geo/map-providers.ts";

/** 两点间距离（米），只用于断言偏移量级 */
function metres(aLng, aLat, bLng, bLat) {
  const toRad = (d) => (d * Math.PI) / 180;
  const dLat = toRad(bLat - aLat);
  const dLng = toRad(bLng - aLng);
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(toRad(aLat)) * Math.cos(toRad(bLat)) * Math.sin(dLng / 2) ** 2;
  return 2 * 6371008.8 * Math.asin(Math.min(1, Math.sqrt(h)));
}

describe("GCJ-02 基准转换", () => {
  it("境内点向东北偏移数百米", () => {
    // 天安门。这组偏移量是公开可核对的定值，写死在这里就是为了钉住正负号：
    // 反了的话纠偏会把底图推到反方向，误差从 0 变成两倍。
    const [lng, lat] = wgs84ToGcj02(116.3912, 39.9067);
    assert.ok(lng > 116.3912, "GCJ-02 经度应大于 WGS-84");
    assert.ok(lat > 39.9067, "GCJ-02 纬度应大于 WGS-84");
    assert.ok(Math.abs(lng - 116.3912 - 0.00624) < 5e-4, `经度偏移异常：${lng - 116.3912}`);
    assert.ok(Math.abs(lat - 39.9067 - 0.0014) < 5e-4, `纬度偏移异常：${lat - 39.9067}`);

    const shift = metres(116.3912, 39.9067, lng, lat);
    assert.ok(shift > 300 && shift < 700, `偏移量应在数百米量级，实际 ${shift.toFixed(0)}m`);
  });

  it("境外不偏移", () => {
    for (const [lng, lat] of [
      [139.6917, 35.6895], // 东京
      [-122.4194, 37.7749] // 旧金山
    ]) {
      assert.equal(outOfChina(lng, lat), true);
      assert.deepEqual(wgs84ToGcj02(lng, lat), [lng, lat]);
      assert.deepEqual(gcj02ToWgs84(lng, lat), [lng, lat]);
    }
  });

  it("往返残差在厘米以内", () => {
    // 逆变换是迭代逼近的，迭代轮数改动后这条会立刻变红
    for (const [lng, lat] of [
      [116.3912, 39.9067],
      [121.4737, 31.2304],
      [87.6168, 43.8256],
      [91.1409, 29.6456]
    ]) {
      const [gLng, gLat] = wgs84ToGcj02(lng, lat);
      const [bLng, bLat] = gcj02ToWgs84(gLng, gLat);
      assert.ok(metres(lng, lat, bLng, bLat) < 0.01, `往返残差过大：${lng},${lat}`);
    }
  });
});

describe("浏览器语言 → 注记语言 + 供应商倾向", () => {
  it("简体中文走境内档", () => {
    for (const tags of [["zh-CN", "en"], ["zh"], ["zh-Hans-CN"], ["zh-hans"]]) {
      assert.deepEqual(
        { ...detectMapLocale(tags), tag: undefined },
        { lang: "zh-CN", region: "cn", tag: undefined },
        `${tags} 应判为境内`
      );
    }
  });

  it("繁体中文要中文注记，但仍走全球档", () => {
    // 走境内线路对港澳台只会更慢。合并成一个维度必然牺牲其中一边，这里选择分开判。
    for (const tags of [["zh-TW"], ["zh-HK"], ["zh-Hant"]]) {
      const locale = detectMapLocale(tags);
      assert.equal(locale.lang, "zh-CN", `${tags} 仍应给中文注记`);
      assert.equal(locale.region, "global", `${tags} 不应走境内供应商`);
    }
  });

  it("非中文取第一条有效语言，空列表退回英文", () => {
    assert.deepEqual(detectMapLocale(["en-US", "zh-CN"]), { lang: "en", region: "global", tag: "en-US" });
    assert.deepEqual(detectMapLocale(["de", "zh-CN"]), { lang: "en", region: "global", tag: "de" });
    assert.deepEqual(detectMapLocale([]), { lang: "en", region: "global", tag: "en" });
    assert.deepEqual(detectMapLocale([""]), { lang: "en", region: "global", tag: "en" });
  });
});

describe("偏好 → 实际供应商", () => {
  it("自动档按大区给出默认供应商", () => {
    assert.equal(resolveProvider("auto", "cn").id, "amap");
    assert.equal(resolveProvider("auto", "global").id, "carto");
  });

  it("显式选择压过语言推导", () => {
    assert.equal(resolveProvider("osm", "cn").id, "osm");
    assert.equal(resolveProvider("amap", "global").id, "amap");
  });

  it("缺密钥或不存在的选择退回自动档", () => {
    // 测试环境没有 NEXT_PUBLIC_MAP_TIANDITU_KEY：选了也不能生效，
    // 必须退回而不是留在一个画不出瓦片的选择上
    assert.equal(resolveProvider("tianditu", "cn").id, "amap");
    assert.equal(resolveProvider("已下架的供应商", "global").id, "carto");
  });
});

describe("供应商目录自身的完整性", () => {
  const ctx = (over = {}) => ({ theme: "light", lang: "zh-CN", key: "TEST_KEY", ...over });

  it("id 唯一", () => {
    const ids = MAP_PROVIDERS.map((p) => p.id);
    assert.equal(new Set(ids).size, ids.length);
  });

  it("每一层瓦片都是 https 且带完整的 xyz 占位", () => {
    for (const provider of MAP_PROVIDERS) {
      for (const layer of provider.build(ctx())) {
        assert.ok(layer.tiles.length > 0, `${provider.id}/${layer.key} 没有瓦片地址`);
        for (const url of layer.tiles) {
          // 控制台跑在 https 下，混合内容会被浏览器直接拦掉且只在控制台留一行警告
          assert.ok(url.startsWith("https://"), `${provider.id} 的瓦片不是 https：${url}`);
          for (const token of ["{x}", "{y}", "{z}"]) {
            assert.ok(url.includes(token), `${provider.id} 的瓦片缺少 ${token}：${url}`);
          }
        }
        assert.ok(layer.tileSize > 0 && layer.maxZoom > 0, `${provider.id}/${layer.key} 尺寸或层级非法`);
      }
    }
  });

  it("声明了原生深色版的供应商，深浅两版地址必须不同", () => {
    for (const provider of MAP_PROVIDERS) {
      if (!provider.nativeDark) continue;
      const light = JSON.stringify(provider.build(ctx({ theme: "light" })));
      const dark = JSON.stringify(provider.build(ctx({ theme: "dark" })));
      // 影像类底图本来就没有深浅之分，但它们也不该被压暗，因此同样标 nativeDark
      const imagery = provider.id.includes("satellite") || provider.id.includes("imagery");
      if (imagery || provider.group === "offline") continue;
      assert.notEqual(light, dark, `${provider.id} 标了原生深色版却给出同一批地址`);
    }
  });

  it("声明了双语注记的供应商，中英文地址必须不同", () => {
    for (const provider of MAP_PROVIDERS) {
      if (provider.langs.length < 2 || provider.group === "offline") continue;
      const zh = JSON.stringify(provider.build(ctx({ lang: "zh-CN" })));
      const en = JSON.stringify(provider.build(ctx({ lang: "en" })));
      assert.notEqual(zh, en, `${provider.id} 标了双语注记却给出同一批地址`);
    }
  });

  it("注记语言取供应商支持的那一档", () => {
    const amap = MAP_PROVIDERS.find((p) => p.id === "amap");
    const carto = MAP_PROVIDERS.find((p) => p.id === "carto");
    assert.equal(effectiveLang(amap, "zh-CN"), "zh-CN");
    // Carto 只有一套注记，中文用户也只能拿到它 —— 界面上不该假装能切
    assert.equal(effectiveLang(carto, "zh-CN"), "en");
  });

  it("需要密钥的供应商必须把密钥拼进地址", () => {
    for (const provider of MAP_PROVIDERS) {
      if (!provider.credential) continue;
      assert.ok(provider.credential.env.startsWith("NEXT_PUBLIC_"), `${provider.id} 的密钥变量进不了浏览器`);
      const urls = provider.build(ctx()).flatMap((layer) => layer.tiles);
      assert.ok(
        urls.every((url) => url.includes("TEST_KEY")),
        `${provider.id} 声明了需要密钥，却有瓦片地址没带上它`
      );
    }
  });

  it("GCJ-02 供应商只出现在中国大陆一档", () => {
    for (const provider of MAP_PROVIDERS) {
      if (provider.datum !== "gcj02") continue;
      assert.equal(provider.group, "china", `${provider.id} 的坐标系与分组对不上`);
    }
  });
});
