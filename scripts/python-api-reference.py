"""Read the public Python declarations without importing the client."""
import ast
import json
from pathlib import Path

root = Path(__file__).resolve().parents[1] / "packages/stow-s3-py/src/stow_s3"
init = ast.parse((root / "__init__.py").read_text())
public = next(ast.literal_eval(n.value) for n in init.body if isinstance(n, ast.Assign) and any(isinstance(t, ast.Name) and t.id == "__all__" for t in n.targets))
modules = {n.module: n.names for n in init.body if isinstance(n, ast.ImportFrom)}

def signature(node):
    value = ast.parse(ast.unparse(node)).body[0]
    value.body = [ast.Pass()]
    value.decorator_list = []
    return ast.unparse(value).removesuffix("\n    pass")

def describe(node):
    return (ast.get_docstring(node) or "").split("\n\n")[0].replace("\n", " ")

def record(node, name):
    if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
        args = node.args
        parameters = []
        positional = args.posonlyargs + args.args
        defaults = [None] * (len(positional) - len(args.defaults)) + args.defaults
        for arg, default in zip(positional + args.kwonlyargs, defaults + args.kw_defaults):
            if arg.arg in ("self", "cls"):
                continue
            parameters.append({"name": arg.arg, "type": ast.unparse(arg.annotation) if arg.annotation else "Any", "optional": default is not None, "default": ast.unparse(default) if default is not None else None})
        for arg, prefix in [(args.vararg, "*"), (args.kwarg, "**")]:
            if arg:
                parameters.append({"name": prefix + arg.arg, "type": ast.unparse(arg.annotation) if arg.annotation else "Any", "optional": True})
        return {"name": name, "kind": "function", "signature": signature(node), "description": describe(node), "parameters": parameters, "returns": ast.unparse(node.returns) if node.returns else "Any"}
    if isinstance(node, ast.ClassDef):
        methods = []
        fields = []
        for member in node.body:
            if isinstance(member, (ast.FunctionDef, ast.AsyncFunctionDef)) and (not member.name.startswith("_") or member.name in ("__init__", "__enter__", "__exit__")):
                methods.append(record(member, member.name))
            if isinstance(member, ast.AnnAssign) and isinstance(member.target, ast.Name) and not member.target.id.startswith("_"):
                fields.append(ast.unparse(member))
            if isinstance(member, ast.Assign) and all(isinstance(t, ast.Name) and not t.id.startswith("_") for t in member.targets):
                fields.append(ast.unparse(member))
        bases = ", ".join(ast.unparse(b) for b in node.bases)
        code = f"class {name}" + (f"({bases})" if bases else "") + ":\n" + ("\n".join("    " + f for f in fields) if fields else "    pass")
        return {"name": name, "kind": "type", "signature": code, "description": describe(node), "methods": methods}
    return {"name": name, "kind": "constant", "signature": ast.unparse(node), "description": ""}

nodes = {"__version__": next(n for n in init.body if isinstance(n, ast.Assign) and any(isinstance(t, ast.Name) and t.id == "__version__" for t in n.targets))}
for module, names in modules.items():
    source = ast.parse((root / (module + ".py")).read_text())
    for item in names:
        for node in source.body:
            if getattr(node, "name", None) == item.name or (isinstance(node, (ast.Assign, ast.AnnAssign)) and any(isinstance(t, ast.Name) and t.id == item.name for t in (node.targets if isinstance(node, ast.Assign) else [node.target]))):
                nodes[item.asname or item.name] = node
print(json.dumps([record(nodes[name], name) for name in public]))
