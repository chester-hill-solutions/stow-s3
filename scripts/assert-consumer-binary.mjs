import assert from "node:assert/strict";
import { realpathSync } from "node:fs";
import { dirname, isAbsolute, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

export function assertConsumerBinary(resolveStowBinary, consumerURL) {
  const root = realpathSync(dirname(fileURLToPath(consumerURL)));
  const carrier = `@chester-hill-solutions/stow-s3-${process.platform}-${process.arch}`;
  let expected;
  try { expected = realpathSync(join(root, "node_modules", carrier, "bin/stow-s3")); }
  catch { throw Error(`clean consumer is missing its installed host carrier: ${carrier}`); }
  const inside = relative(root, expected);
  assert.ok(inside && !inside.startsWith("..") && !isAbsolute(inside), "host carrier must be inside the clean consumer");
  assert.equal(realpathSync(resolveStowBinary()), expected, "SDK must select this consumer's installed host carrier");
  return expected;
}
