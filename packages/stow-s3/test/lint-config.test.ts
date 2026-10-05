import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { ESLint } from "eslint";

test("the migrated import plugin still rejects duplicate imports", async () => {
  const eslint = new ESLint({ cwd: fileURLToPath(new URL("..", import.meta.url)) });
  const [duplicate] = await eslint.lintText(
    'import { join } from "node:path";\nimport { resolve } from "node:path";\nexport const result = join(resolve("a"), "b");\n',
    { filePath: "src/import-rule-probe.ts" },
  );
  assert.ok(duplicate);
  assert.ok(duplicate.messages.some((message) => message.ruleId === "import/no-duplicates" && message.severity === 2));
  const [merged] = await eslint.lintText(
    'import { join, resolve } from "node:path";\nexport const result = join(resolve("a"), "b");\n',
    { filePath: "src/import-rule-probe.ts" },
  );
  assert.ok(merged);
  assert.equal(merged.errorCount, 0);
});
