import { expect } from "@playwright/test";
import { randomUUID } from "node:crypto";

export function testKey() { return `cases/${randomUUID()}.txt`; }

export async function openHarness(page, options = {}) {
  const params = new URLSearchParams({ namespace: options.namespace ?? randomUUID() });
  if (options.maxBytes) params.set("maxBytes", String(options.maxBytes));
  if (options.direct) params.set("direct", "1");
  await page.goto(`/test/harness.html?${params}`);
  await expect.poll(() => page.evaluate(() => Boolean(window.qa?.ready || window.qa?.error))).toBe(true);
  return page.evaluate(() => window.qa.error);
}

export function invoke(page, method, key, text, options) {
  return page.evaluate((args) => window.qa.call(...args), [method, key, text, options]);
}

export async function reloadHarness(page) {
  await page.reload();
  await expect.poll(() => page.evaluate(() => window.qa?.ready)).toBe(true);
}

export async function remoteStats(request, key) {
  const response = await request.get(`/test/stats?key=${encodeURIComponent(key)}`);
  return response.json();
}
