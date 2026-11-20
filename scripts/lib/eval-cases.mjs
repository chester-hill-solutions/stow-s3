import { existsSync, readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";

export function parseFrontmatter(text) {
  if (!text.startsWith("---\n")) return { fields: {}, body: text.trim(), error: "no frontmatter" };
  const end = text.indexOf("\n---\n", 4);
  if (end === -1) return { fields: {}, body: "", error: "frontmatter is never closed" };
  const fields = {};
  for (const line of text.slice(4, end).split("\n")) {
    const match = /^([a-z_]+):\s*(.*)$/.exec(line);
    if (match) fields[match[1]] = match[2];
  }
  return { fields, body: text.slice(end + 5).trim() };
}

export function discoverCases(casesDir) {
  if (!existsSync(casesDir)) return [];
  return readdirSync(casesDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && entry.name !== "results")
    .map((entry) => {
      const dir = resolve(casesDir, entry.name);
      const promptPath = resolve(dir, "prompt.md");
      if (!existsSync(promptPath)) return null;
      const prompt = parseFrontmatter(readFileSync(promptPath, "utf8"));
      const gradersDir = resolve(dir, "graders");
      const graders = existsSync(gradersDir)
        ? readdirSync(gradersDir)
            .filter((name) => name.endsWith(".md"))
            .map((name) => ({ name: name.slice(0, -3), ...parseFrontmatter(readFileSync(resolve(gradersDir, name), "utf8")) }))
        : [];
      const tags = prompt.fields.tags ?? "";
      return {
        name: entry.name,
        dir,
        fields: prompt.fields,
        prompt: prompt.body,
        runs: Number(prompt.fields.runs ?? 3),
        maxTurns: prompt.fields.max_turns,
        timeoutSeconds: Number(prompt.fields.timeout_seconds ?? 300),
        tags,
        kind: tags.includes("negative") ? "negative" : tags.includes("positive") ? "positive" : "untagged",
        heldout: tags.includes("heldout"),
        graders,
      };
    })
    .filter(Boolean);
}
