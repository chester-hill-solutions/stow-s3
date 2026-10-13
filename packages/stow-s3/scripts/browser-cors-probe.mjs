import { Readable } from "node:stream";
import { inspectCors } from "./browser-remote.mjs";
import { signBrowserTestObject } from "./browser-signing.mjs";

export async function waitForBrowserCors(fixture) {
  const origin = "http://127.0.0.1:4180";
  const started = Date.now();
  const saveId = crypto.randomUUID();
  let basis = { absent: true };
  let last;
  for (let attempt = 1; Date.now() - started < 90_000; attempt += 1) {
    const signed = await signBrowserTestObject(fixture, Readable.from([JSON.stringify({ method: "PUT", key: "cors-probe.txt", options: {
      basis, saveId, contentType: "text/plain", metadata: { purpose: "cors" },
    } })]));
    const options = await fetch(signed.url, { method: "OPTIONS", headers: {
      Origin: origin, "access-control-request-method": "PUT", "access-control-request-headers": Object.keys(signed.headers).join(","),
    }, signal: AbortSignal.timeout(10_000) });
    await options.arrayBuffer();
    last = { preflightStatus: options.status, preflightOrigin: options.headers.get("access-control-allow-origin") };
    if (options.ok && options.headers.get("access-control-allow-origin") === origin) {
      const put = await fetch(signed.url, { method: "PUT", body: new TextEncoder().encode("cors probe"), headers: { ...signed.headers, Origin: origin }, signal: AbortSignal.timeout(10_000) });
      const body = await put.text();
      last = { ...last, putStatus: put.status, putOrigin: put.headers.get("access-control-allow-origin"), expose: put.headers.get("access-control-expose-headers"), code: /<Code>([^<]+)/.exec(body)?.[1] };
      if (put.ok) basis = { etag: put.headers.get("etag") };
      const exposed = put.headers.get("access-control-expose-headers")?.toLowerCase().split(",").map((value) => value.trim()) ?? [];
      if (put.ok && put.headers.get("access-control-allow-origin") === origin && exposed.includes("etag") && exposed.includes("x-amz-meta-stow-save-id")) {
        const result = { corsReady: true, checks: attempt, elapsedMs: Date.now() - started };
        if (process.env.STOW_BROWSER_CORS_PROBE === "1") result.rules = await inspectCors(fixture.client, fixture.bucket);
        process.stdout.write(JSON.stringify(result) + "\n"); return;
      }
    }
    if (process.env.STOW_BROWSER_CORS_PROBE === "1" && attempt === 1) process.stdout.write(JSON.stringify(last) + "\n");
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`Railway CORS did not become visible on actual responses: ${JSON.stringify(last)}`);
}
