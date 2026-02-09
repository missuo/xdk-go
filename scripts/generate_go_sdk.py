#!/usr/bin/env python3
import ast
import glob
import os
import re
from dataclasses import dataclass
from typing import Dict, List, Optional

BASE = "xdk-python/xdk"


@dataclass
class Operation:
    tag: str
    method_name: str
    go_method_name: str
    http_method: str
    path: str
    path_params: List[str]
    query_params: Dict[str, str]
    required_params: List[str]
    all_params: List[str]
    security: List[str]
    has_body: bool
    paginated: bool
    streaming: bool
    pagination_param: str



def snake_to_pascal(name: str) -> str:
    return "".join(part[:1].upper() + part[1:] for part in name.split("_"))



def extract_guard_param(test: ast.AST) -> Optional[str]:
    if not isinstance(test, ast.Compare):
        return None
    if len(test.ops) != 1 or not isinstance(test.ops[0], ast.IsNot):
        return None
    if len(test.comparators) != 1:
        return None
    if not isinstance(test.comparators[0], ast.Constant) or test.comparators[0].value is not None:
        return None
    if not isinstance(test.left, ast.Name):
        return None
    return test.left.id



def subscript_key(node: ast.AST) -> Optional[str]:
    if not isinstance(node, ast.Subscript):
        return None
    if not isinstance(node.value, ast.Name):
        return None
    if node.value.id not in ("params", "page_params"):
        return None
    sl = node.slice
    if isinstance(sl, ast.Constant) and isinstance(sl.value, str):
        return sl.value
    return None



def names_in_expr(node: ast.AST) -> List[str]:
    names = []
    for n in ast.walk(node):
        if isinstance(n, ast.Name):
            names.append(n.id)
    return names



def extract_query_params(fn: ast.FunctionDef, argset: set[str]) -> Dict[str, str]:
    result: Dict[str, str] = {}

    def visit(node: ast.AST, guard: Optional[str] = None) -> None:
        if isinstance(node, ast.If):
            new_guard = extract_guard_param(node.test) or guard
            for stmt in node.body:
                visit(stmt, new_guard)
            for stmt in node.orelse:
                visit(stmt, guard)
            return

        if isinstance(node, ast.Assign):
            key: Optional[str] = None
            for target in node.targets:
                key = subscript_key(target)
                if key:
                    break
            if key:
                param = guard
                if not param:
                    for n in names_in_expr(node.value):
                        if n in argset:
                            param = n
                            break
                if param and param in argset and param not in ("body", "stream_config"):
                    result[param] = key
            return

        for child in ast.iter_child_nodes(node):
            visit(child, guard)

    visit(fn)
    return result



def parse_file(path: str) -> tuple[str, List[Operation]]:
    tag = os.path.basename(os.path.dirname(path))
    src = open(path, "r", encoding="utf-8").read()
    lines = src.splitlines()
    tree = ast.parse(src)

    cls = None
    for n in tree.body:
        if isinstance(n, ast.ClassDef) and n.name.endswith("Client"):
            cls = n
            break
    if cls is None:
        return tag, []

    ops: List[Operation] = []
    for fn in cls.body:
        if not isinstance(fn, ast.FunctionDef) or fn.name == "__init__":
            continue

        seg = "\n".join(lines[fn.lineno - 1 : fn.end_lineno])

        args = [a.arg for a in fn.args.args[1:]]
        defaults_count = len(fn.args.defaults)
        required = args[: len(args) - defaults_count] if defaults_count <= len(args) else args

        m = re.search(r'url\s*=\s*self\.client\.base_url\s*\+\s*"([^"]+)"', seg)
        path_template = m.group(1) if m else ""

        path_params = []
        for p in re.findall(r'url\s*=\s*url\.replace\("\{([^}]+)\}"\s*,\s*str\((\w+)\)\)', seg):
            path_params.append(p[1])

        method_match = re.search(r"self\.client\.session\.(get|post|put|delete|patch)\(", seg)
        method = method_match.group(1).upper() if method_match else "GET"

        if "stream_with_retry(" in seg:
            mm = re.search(r'method\s*=\s*"(get|post|put|delete|patch)"', seg)
            if mm:
                method = mm.group(1).upper()

        security = []
        for scheme in re.findall(r"(?:page_)?acceptable_schemes\.append\(\"([^\"]+)\"\)", seg):
            if scheme not in security:
                security.append(scheme)

        query_params = extract_query_params(fn, set(args))

        paginated = "yield page_response" in seg and "while True" in seg
        streaming = "stream_with_retry(" in seg

        pm = re.search(r'pagination_param_name\s*=\s*"([^"]+)"', seg)
        pagination_param = pm.group(1) if pm else "pagination_token"

        op = Operation(
            tag=tag,
            method_name=fn.name,
            go_method_name=snake_to_pascal(fn.name),
            http_method=method,
            path=path_template,
            path_params=path_params,
            query_params=query_params,
            required_params=required,
            all_params=args,
            security=security,
            has_body=("body" in args),
            paginated=paginated,
            streaming=streaming,
            pagination_param=pagination_param,
        )
        ops.append(op)

    return tag, ops



def go_string(s: str) -> str:
    s = s.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{s}"'



def write_operations_file(ops_by_tag: Dict[str, List[Operation]]) -> None:
    out = []
    out.append("// Code generated by scripts/generate_go_sdk.py; DO NOT EDIT.")
    out.append("package xdk")
    out.append("")
    out.append("type operation struct {")
    out.append("\tTag             string")
    out.append("\tName            string")
    out.append("\tMethod          string")
    out.append("\tPath            string")
    out.append("\tPathParams      []string")
    out.append("\tQueryParams     map[string]string")
    out.append("\tRequiredParams  []string")
    out.append("\tAllParams       []string")
    out.append("\tSecuritySchemes []string")
    out.append("\tHasBody         bool")
    out.append("\tPaginated       bool")
    out.append("\tStreaming       bool")
    out.append("\tPaginationParam string")
    out.append("}")
    out.append("")
    out.append("var operations = map[string]operation{")

    for tag in sorted(ops_by_tag):
        for op in ops_by_tag[tag]:
            key = f"{tag}.{op.method_name}"
            out.append(f"\t{go_string(key)}: {{")
            out.append(f"\t\tTag: {go_string(tag)},")
            out.append(f"\t\tName: {go_string(op.method_name)},")
            out.append(f"\t\tMethod: {go_string(op.http_method)},")
            out.append(f"\t\tPath: {go_string(op.path)},")

            out.append("\t\tPathParams: []string{")
            for p in op.path_params:
                out.append(f"\t\t\t{go_string(p)},")
            out.append("\t\t},")

            out.append("\t\tQueryParams: map[string]string{")
            for p, q in sorted(op.query_params.items()):
                out.append(f"\t\t\t{go_string(p)}: {go_string(q)},")
            out.append("\t\t},")

            out.append("\t\tRequiredParams: []string{")
            for p in op.required_params:
                out.append(f"\t\t\t{go_string(p)},")
            out.append("\t\t},")

            out.append("\t\tAllParams: []string{")
            for p in op.all_params:
                out.append(f"\t\t\t{go_string(p)},")
            out.append("\t\t},")

            out.append("\t\tSecuritySchemes: []string{")
            for s in op.security:
                out.append(f"\t\t\t{go_string(s)},")
            out.append("\t\t},")

            out.append(f"\t\tHasBody: {str(op.has_body).lower()},")
            out.append(f"\t\tPaginated: {str(op.paginated).lower()},")
            out.append(f"\t\tStreaming: {str(op.streaming).lower()},")
            out.append(f"\t\tPaginationParam: {go_string(op.pagination_param)},")
            out.append("\t},")

    out.append("}")
    out.append("")
    with open("operations_gen.go", "w", encoding="utf-8") as f:
        f.write("\n".join(out) + "\n")



def write_tag_file(tag: str, ops: List[Operation]) -> None:
    struct_name = f"{snake_to_pascal(tag)}Client"

    out: List[str] = []
    out.append("// Code generated by scripts/generate_go_sdk.py; DO NOT EDIT.")
    out.append("package xdk")
    out.append("")

    needs_context = any((not op.paginated and not op.streaming) or op.streaming for op in ops)
    if needs_context:
        out.append("import \"context\"")
        out.append("")

    out.append(f"type {struct_name} struct {{")
    out.append("\tclient *Client")
    out.append("}")
    out.append("")

    for op in ops:
        op_key = f"{tag}.{op.method_name}"

        if op.streaming:
            out.append(
                f"func (c *{struct_name}) {op.go_method_name}(ctx context.Context, input Params, config *StreamConfig) (<-chan JSON, <-chan error) {{"
            )
            out.append("\treturn c.client.stream(ctx, operations[\"%s\"], cloneParams(input), config)" % op_key)
            out.append("}")
            out.append("")
            continue

        if op.paginated:
            out.append(f"func (c *{struct_name}) {op.go_method_name}(input Params) *Pager {{")
            out.append("\treturn c.client.newPager(operations[\"%s\"], cloneParams(input))" % op_key)
            out.append("}")
            out.append("")
            continue

        out.append(f"func (c *{struct_name}) {op.go_method_name}(ctx context.Context, input Params) (JSON, error) {{")
        out.append("\treturn c.client.call(ctx, operations[\"%s\"], cloneParams(input))" % op_key)
        out.append("}")
        out.append("")

    with open(f"{tag}_client_gen.go", "w", encoding="utf-8") as f:
        f.write("\n".join(out) + "\n")



def write_tags_file(tags: List[str]) -> None:
    out: List[str] = []
    out.append("// Code generated by scripts/generate_go_sdk.py; DO NOT EDIT.")
    out.append("package xdk")
    out.append("")
    out.append("var generatedTags = []string{")
    for tag in tags:
        out.append(f"\t{go_string(tag)},")
    out.append("}")
    out.append("")

    with open("tags_gen.go", "w", encoding="utf-8") as f:
        f.write("\n".join(out) + "\n")



def main() -> None:
    ops_by_tag: Dict[str, List[Operation]] = {}
    for path in sorted(glob.glob(os.path.join(BASE, "*", "client.py"))):
        tag, ops = parse_file(path)
        if ops:
            ops_by_tag[tag] = ops

    write_operations_file(ops_by_tag)

    for tag in sorted(ops_by_tag):
        write_tag_file(tag, ops_by_tag[tag])

    write_tags_file(sorted(ops_by_tag))

    total = sum(len(v) for v in ops_by_tag.values())
    print(f"generated {len(ops_by_tag)} tags and {total} operations")


if __name__ == "__main__":
    main()
