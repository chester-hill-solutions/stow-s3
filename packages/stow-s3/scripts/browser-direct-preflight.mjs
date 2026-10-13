import { Readable } from "node:stream";
import { openBrowserTestRemote } from "./browser-remote.mjs";
import { signBrowserTestObject } from "./browser-signing.mjs";

const fixture = await openBrowserTestRemote();
try {
  const signed = await signBrowserTestObject(fixture, Readable.from([JSON.stringify({ method: "PUT", key: "signed-probe.txt", options: {
    basis: { absent: true }, saveId: crypto.randomUUID(), contentType: "text/plain", metadata: { purpose: "direct" },
  } })]));
  const response = await fetch(signed.url, { method: "PUT", body: new TextEncoder().encode("signed bytes"), headers: signed.headers });
  const body = await response.text();
  process.stdout.write(JSON.stringify({ status: response.status, code: /<Code>([^<]+)/.exec(body)?.[1], signedHeaders: new URL(signed.url).searchParams.get("X-Amz-SignedHeaders"), queryFields: [...new URL(signed.url).searchParams.keys()] }, null, 2) + "\n");
  if (!response.ok) process.exitCode = 1;
} finally { await fixture.close(); }
