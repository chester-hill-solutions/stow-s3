import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fieldNotes, qualifiedFieldNotes } from "../site/api-notes.mjs";

const require = createRequire(resolve("packages/stow-s3/package.json"));
const ts = require("typescript");
const printer = ts.createPrinter({ removeComments: true });
const flags = ts.TypeFormatFlags.NoTruncation | ts.TypeFormatFlags.UseAliasDefinedOutsideCurrentScope;

export function firstSentence(text) {
  return text.trim().split(/\n\s*\n/)[0].replaceAll(/\s+/g, " ").split(/(?<=\.)\s/)[0];
}

export function typescriptCatalog() {
  const exports = JSON.parse(readFileSync("packages/stow-s3/package.json", "utf8")).exports;
  const paths = Object.fromEntries(Object.entries(exports).map(([key, value]) => [key, value.types.replace("./dist/", "packages/stow-s3/src/").replace(/\.d\.ts$/, ".ts")]));
  const program = ts.createProgram(Object.values(paths), { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.NodeNext, strict: true, skipLibCheck: true });
  const checker = program.getTypeChecker();
  const typeText = (type) => checker.typeToString(type, undefined, flags).replace(/import\("[^"]+"\)\./g, "");
  const documentation = (symbol) => firstSentence(ts.displayPartsToString(symbol?.getDocumentationComment(checker) ?? []));
  const privateMember = (node) => node.modifiers?.some((m) => m.kind === ts.SyntaxKind.PrivateKeyword || m.kind === ts.SyntaxKind.ProtectedKeyword) || node.name && ts.isPrivateIdentifier(node.name);
  function callable(node, name) {
    const signature = checker.getSignatureFromDeclaration(node);
    return {
      name,
      signature: `${name}${checker.signatureToString(signature, undefined, flags)}`,
      parameters: node.parameters.map((p) => ({ name: p.name.getText(), type: typeText(checker.getTypeAtLocation(p)), optional: !!p.questionToken || !!p.initializer, default: p.initializer?.getText() })),
      returns: typeText(checker.getReturnTypeOfSignature(signature)),
      description: documentation(node.name && checker.getSymbolAtLocation(node.name)),
    };
  }
  function describe(symbol) {
    const actual = symbol.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(symbol) : symbol;
    const node = actual.declarations?.find((d) => !ts.isExportSpecifier(d)) ?? actual.valueDeclaration;
    if (!node) throw new Error(`No declaration for ${symbol.name}`);
    const item = { name: symbol.name, description: documentation(actual), methods: [], fields: [] };
    if (ts.isFunctionDeclaration(node)) return { ...item, kind: "function", ...callable(node, symbol.name) };
    if (ts.isClassDeclaration(node) || ts.isInterfaceDeclaration(node)) {
      item.kind = ts.isClassDeclaration(node) ? "class" : "interface";
      const signatures = [];
      for (const member of node.members) {
        if (privateMember(member)) continue;
        if (ts.isMethodDeclaration(member) || ts.isMethodSignature(member) || ts.isConstructorDeclaration(member)) {
          const name = ts.isConstructorDeclaration(member) ? "constructor" : member.name.getText();
          const detail = callable(member, name);
          if (ts.isConstructorDeclaration(member)) { detail.returns = symbol.name; detail.signature = detail.signature.slice(0, detail.signature.lastIndexOf("):")) + ")"; }
          item.methods.push(detail);
          signatures.push(`${member.modifiers?.some((m) => m.kind === ts.SyntaxKind.StaticKeyword) ? "static " : ""}${detail.signature};`);
        } else {
          const name = member.name?.getText();
          if (name) signatures.push(`${member.modifiers?.some((m) => m.kind === ts.SyntaxKind.ReadonlyKeyword) || ts.isGetAccessorDeclaration(member) ? "readonly " : ""}${name}${member.questionToken ? "?" : ""}: ${typeText(checker.getTypeAtLocation(member))};`);
        }
      }
      const inheritance = node.heritageClauses?.map((h) => printer.printNode(ts.EmitHint.Unspecified, h, node.getSourceFile())).join(" ") ?? "";
      item.signature = `${item.kind} ${symbol.name}${inheritance ? " " + inheritance : ""} {\n${signatures.map((s) => "  " + s.replaceAll("\n", "\n  ")).join("\n")}\n}`;
      const instance = checker.getDeclaredTypeOfSymbol(actual);
      for (const field of instance.getProperties()) {
        const declaration = field.valueDeclaration ?? field.declarations?.[0];
        if (!declaration || privateMember(declaration) || declaration.kind === ts.SyntaxKind.MethodDeclaration || declaration.kind === ts.SyntaxKind.MethodSignature || field.name.startsWith("__@")) continue;
        item.fields.push({ name: field.name, type: typeText(checker.getTypeOfSymbolAtLocation(field, declaration)), optional: !!(field.flags & ts.SymbolFlags.Optional), description: qualifiedFieldNotes[`${symbol.name}.${field.name}`] ?? fieldNotes[field.name] ?? documentation(field) });
      }
      return item;
    }
    if (ts.isTypeAliasDeclaration(node)) return { ...item, kind: "type", signature: printer.printNode(ts.EmitHint.Unspecified, node, node.getSourceFile()).replace(/^export /, "") };
    const type = checker.getTypeOfSymbolAtLocation(actual, node);
    item.kind = "value";
    item.signature = `const ${symbol.name === "default" ? "Stow" : symbol.name}: ${typeText(type)};`;
    for (const member of type.getProperties()) {
      const declaration = member.valueDeclaration ?? member.declarations?.[0];
      if (declaration && ts.isMethodDeclaration(declaration)) item.methods.push(callable(declaration, member.name));
    }
    return item;
  }
  return Object.fromEntries(Object.entries(paths).map(([entry, path]) => {
    const file = program.getSourceFile(path);
    const module = checker.getSymbolAtLocation(file);
    return [entry, checker.getExportsOfModule(module).map(describe).sort((a, b) => a.name.localeCompare(b.name))];
  }));
}
