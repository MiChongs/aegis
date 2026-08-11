import fs from "node:fs";
import path from "node:path";
import worldModule from "geojson-world-map/lib/world.js";

const rootDir = path.resolve(import.meta.dirname, "..");
const publicGeoDir = path.join(rootDir, "public", "geo");
const chinaGeoDir = path.join(publicGeoDir, "china");
const chinaSourceDir = path.join(rootDir, "node_modules", "china-map-echarts", "map");
const chinaCodeMapPath = path.join(rootDir, "node_modules", "china-map-echarts", "codeToNameMap.json");
const worldOutputPath = path.join(publicGeoDir, "world-countries.geo.json");

fs.mkdirSync(publicGeoDir, { recursive: true });
fs.mkdirSync(chinaGeoDir, { recursive: true });

const worldGeoJson = worldModule.default || worldModule;
fs.writeFileSync(worldOutputPath, JSON.stringify(worldGeoJson));

for (const entry of fs.readdirSync(chinaSourceDir, { withFileTypes: true })) {
  if (!entry.isFile() || !entry.name.endsWith(".json")) {
    continue;
  }
  fs.copyFileSync(path.join(chinaSourceDir, entry.name), path.join(chinaGeoDir, entry.name));
}

fs.copyFileSync(chinaCodeMapPath, path.join(chinaGeoDir, "code-to-name-map.json"));

const chinaCount = fs.readdirSync(chinaGeoDir).filter((item) => item.endsWith(".json")).length;
console.log(`geo assets synced: world=1 china=${chinaCount}`);
