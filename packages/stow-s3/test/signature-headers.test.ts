import assert from "node:assert/strict";
import { test } from "node:test";
import { SignatureV4 } from "@smithy/signature-v4";
import { SHA256 } from "./shared-corpus-signed.js";

for (const mode of ["sign", "presign"] as const) {
  test(`${mode} authenticates Object-prototype-named headers without prototype mutation`, async () => {
    const signer = new SignatureV4({
      credentials: { accessKeyId: "LOCALTEST", secretAccessKey: "local-test-credential" },
      region: "us-east-1", service: "s3", sha256: SHA256,
    });
    const request = { protocol: "https:", hostname: "example.invalid", method: "GET", path: "/bucket/object", query: {} };
    const options = { signingDate: new Date("2026-01-02T03:04:05Z") };
    const sign = (headers: Record<string, string>) => mode === "sign"
      ? signer.sign({ ...request, headers }, options)
      : signer.presign({ ...request, headers }, options);
    const headers = Object.fromEntries([["host", "example.invalid"], ["constructor", "ctor-value"], ["__proto__", "proto-value"]]);
    const receipt = await sign(headers);
    const names = mode === "sign" ? receipt.headers.authorization?.match(/SignedHeaders=([^,]+)/)?.[1] : receipt.query?.["X-Amz-SignedHeaders"];
    assert.equal(typeof names, "string");
    for (const name of ["constructor", "__proto__"]) {
      assert.ok(typeof names === "string" && names.split(";").includes(name), `${name} is missing from ${String(names)}`);
      const changed = await sign(Object.fromEntries(Object.entries(headers).map(([key, value]) => [key, key === name ? "changed" : value])));
      assert.notDeepEqual(mode === "sign" ? changed.headers.authorization : changed.query?.["X-Amz-Signature"],
        mode === "sign" ? receipt.headers.authorization : receipt.query?.["X-Amz-Signature"]);
    }
    assert.equal(Object.getPrototypeOf(headers), Object.prototype);
    assert.equal(headers["__proto__"], "proto-value");
  });
}
