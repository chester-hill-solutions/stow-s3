import { test, expect } from "@playwright/test";

test("the editor reloads offline from its service worker and preserves edits and undo/redo", async ({ page, context }) => {
  await page.goto("/");
  await expect(page.getByText("Available offline", { exact: true })).toBeVisible();
  const editor = page.getByRole("textbox", { name: "File contents" });
  await editor.fill("first durable stage"); await expect(page.getByText("Saved on this device", { exact: true })).toBeVisible();
  await editor.fill("second durable stage"); await expect(page.getByText("Saved on this device", { exact: true })).toBeVisible();
  await context.setOffline(true); await page.reload();
  await expect(editor).toHaveValue("second durable stage");
  await page.getByRole("button", { name: "↶ Undo", exact: true }).click();
  await expect(editor).toHaveValue("first durable stage");
  await editor.fill("edited while offline"); await expect(page.getByText("Saved on this device", { exact: true })).toBeVisible();
  await page.reload(); await expect(editor).toHaveValue("edited while offline");
  await context.setOffline(false);
  await page.getByRole("button", { name: "Sync current stage ↑", exact: true }).click();
  await expect(page.getByText("Remote version confirmed", { exact: true })).toBeVisible();
});
