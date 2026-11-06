import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const ts = require("../packages/stow-s3/node_modules/typescript");
const normalized = (name) => name.replace(/^[_#]+|_+$/g, "").toLowerCase();

function containsType(type, predicate) {
  if (type === undefined) return false;
  if (predicate(type)) return true;
  if (ts.isUnionTypeNode(type)) return type.types.some((member) => containsType(member, predicate));
  return false;
}

function isBoolean(binding) {
  return containsType(binding.type, (type) => type.kind === ts.SyntaxKind.BooleanKeyword) ||
    binding.initializer?.kind === ts.SyntaxKind.TrueKeyword || binding.initializer?.kind === ts.SyntaxKind.FalseKeyword;
}

function isCleanupPromise(binding, names) {
  const name = normalized(binding.name.getText()).replace(/promise$/, "");
  if (!names.has(name)) return false;
  return containsType(binding.type, (type) => ts.isTypeReferenceNode(type) && type.typeName.getText() === "Promise") &&
    (binding.questionToken !== undefined || containsType(binding.type, (type) => type.kind === ts.SyntaxKind.UndefinedKeyword));
}

function bindingsOf(node) {
  if (ts.isClassDeclaration(node) || ts.isClassExpression(node)) return node.members.filter(ts.isPropertyDeclaration);
  const statements = ts.isSourceFile(node) || ts.isBlock(node) ? node.statements : [];
  return statements.filter(ts.isVariableStatement).flatMap((statement) =>
    (statement.declarationList.flags & ts.NodeFlags.Const) === 0 ? [...statement.declarationList.declarations] : []);
}

function namedOwner(node, parent) {
  if (ts.isFunctionLike(node)) {
    const name = node.name?.getText();
    return `${parent}.${name ?? "callback"}`;
  }
  if (ts.isClassDeclaration(node) || ts.isClassExpression(node)) return `${parent}.${node.name?.text ?? "class"}`;
  if (ts.isVariableDeclaration(node)) return `${parent}.${node.name.getText()}`;
  return parent;
}

export function scanTypeScript(path, source, rules) {
  const file = ts.createSourceFile(path, source, ts.ScriptTarget.Latest, true, path.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  if (file.parseDiagnostics.length) {
    throw new Error(`${path}: ${ts.flattenDiagnosticMessageText(file.parseDiagnostics[0].messageText, " ")}`);
  }
  const names = new Set(rules.lifecycleNames);
  const cleanupNames = new Set(rules.cleanupPromiseNames);
  const findings = [];
  const visit = (node, parent) => {
    const owner = namedOwner(node, parent);
    const bindings = bindingsOf(node).filter((binding) => ts.isIdentifier(binding.name) || ts.isPrivateIdentifier(binding.name));
    const flags = bindings.filter(isBoolean).map((binding) => binding.name.getText()).filter((name) => names.has(normalized(name))).sort();
    const promises = bindings.filter((binding) => isCleanupPromise(binding, cleanupNames)).map((binding) => binding.name.getText()).sort();
    const add = (rule, selected) => findings.push({ path, owner, rule, flags: selected, line: file.getLineAndCharacterOfPosition(node.getStart(file)).line + 1 });
    if (flags.length >= 2) add("lifecycle-flags", flags);
    if (flags.length && promises.length) add("lifecycle-cleanup-promise", [...flags, ...promises].sort());
    ts.forEachChild(node, (child) => visit(child, owner));
  };
  visit(file, "module");
  return findings;
}
