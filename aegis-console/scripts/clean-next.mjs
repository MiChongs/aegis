import { existsSync, rmSync } from "node:fs";
import { join } from "node:path";

const targets = [".next", ".next-dev"].map((name) => join(process.cwd(), name));

for (const target of targets) {
  if (!existsSync(target)) {
    continue;
  }

  rmSync(target, {
    force: true,
    recursive: true,
    maxRetries: 3,
    retryDelay: 200
  });
}
