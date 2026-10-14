import hljs from "highlight.js/lib/core";
import bash from "highlight.js/lib/languages/bash";
import go from "highlight.js/lib/languages/go";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import python from "highlight.js/lib/languages/python";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";

for (const [name, grammar] of Object.entries({ bash, go, javascript, json, python, typescript, xml })) {
  hljs.registerLanguage(name, grammar);
}

const aliases = { sh: "bash", shell: "bash", console: "bash", js: "javascript", ts: "typescript", py: "python", html: "xml" };
const labels = { bash: "Shell", go: "Go", javascript: "JavaScript", json: "JSON", python: "Python", typescript: "TypeScript", xml: "XML", plaintext: "Text" };

export function escapeHtml(text) {
  return String(text).replaceAll("&", "&amp;").replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#39;");
}

function highlighted(text, requested) {
  const name = (requested ?? "").split(/\s/)[0].toLowerCase();
  const language = aliases[name] ?? name;
  const supported = hljs.getLanguage(language);
  return {
    language: supported ? language : "plaintext",
    label: labels[language] ?? "Text",
    markup: supported ? hljs.highlight(text, { language, ignoreIllegals: true }).value : escapeHtml(text),
  };
}

function gutter(text) {
  const count = text.replace(/\n$/, "").split("\n").length;
  return `<span class="code-gutter" aria-hidden="true">${Array.from({ length: count }, (_, index) => index + 1).join("\n")}</span>`;
}

export function renderCodeBlock(text, language, id) {
  const result = highlighted(text, language);
  const count = text.replace(/\n$/, "").split("\n").length;
  return `<figure class="code-block"><figcaption class="code-heading"><span class="code-file">${result.label}</span><span class="code-meta">${count} ${count === 1 ? "line" : "lines"}</span><button type="button" data-copy="#${id}" aria-label="Copy ${result.label} code" hidden>Copy</button></figcaption><pre class="stow-code" tabindex="0" aria-label="${result.label} example">${gutter(text)}<code id="${id}" class="hljs language-${result.language}">${result.markup}</code></pre></figure>\n`;
}

// Authored homepage snippets may contain the previous hand-coloured spans.
// Decode each entity once so highlighting preserves the exact copied source.
function sourceText(markup) {
  const entities = { amp: "&", lt: "<", gt: ">", quot: '"', apos: "'" };
  return markup.replace(/<\/?span\b[^>]*>/g, "")
    .replace(/&(amp|lt|gt|quot|apos|#\d+|#x[\da-f]+);/gi, (_, entity) => {
      if (entity[0] !== "#") return entities[entity.toLowerCase()];
      return String.fromCodePoint(entity[1].toLowerCase() === "x" ? parseInt(entity.slice(2), 16) : Number(entity.slice(1)));
    });
}

export function highlightHome(html) {
  return html.replace(/<pre\b([^>]*)><code\b([^>]*)>([\s\S]*?)<\/code><\/pre>/g, (block, preAttributes, codeAttributes, markup) => {
    const language = codeAttributes.match(/data-language="([^"]+)"/)?.[1];
    if (!language) return block;
    const text = sourceText(markup);
    const result = highlighted(text, language);
    return `<pre class="stow-code"${preAttributes}>${gutter(text)}<code class="hljs language-${result.language}"${codeAttributes}>${result.markup}</code></pre>`;
  });
}
