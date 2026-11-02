import { refractor } from "refractor/core";
import go from "refractor/go";
import typescript from "refractor/typescript";
import tsx from "refractor/tsx";
import python from "refractor/python";
import bash from "refractor/bash";
import json from "refractor/json";
import yaml from "refractor/yaml";
import hcl from "refractor/hcl";
import markdown from "refractor/markdown";
import css from "refractor/css";
import sql from "refractor/sql";
import rust from "refractor/rust";
import ruby from "refractor/ruby";
import docker from "refractor/docker";
import toml from "refractor/toml";
import java from "refractor/java";
import cpp from "refractor/cpp";
import csharp from "refractor/csharp";
import swift from "refractor/swift";
import { languageFor } from "./syntax-language";
export { languageFor } from "./syntax-language";
import type { Element, Text } from "hast";
import type {
  HighlightSpan,
  SyntaxKind,
  SyntaxRequest,
  SyntaxResult,
} from "./syntax-types";

for (const grammar of [
  go,
  typescript,
  tsx,
  python,
  bash,
  json,
  yaml,
  hcl,
  markdown,
  css,
  sql,
  rust,
  ruby,
  docker,
  toml,
  java,
  cpp,
  csharp,
  swift,
])
  refractor.register(grammar);

const kinds: Record<string, SyntaxKind> = {
  comment: "comment",
  prolog: "comment",
  doctype: "comment",
  keyword: "keyword",
  boolean: "keyword",
  important: "keyword",
  atrule: "keyword",
  string: "string",
  char: "string",
  "attr-value": "string",
  regex: "string",
  number: "number",
  constant: "number",
  function: "function",
  "function-variable": "function",
  "class-name": "type",
  builtin: "type",
  namespace: "type",
  property: "property",
  "attr-name": "property",
  variable: "property",
  operator: "operator",
  punctuation: "punctuation",
  tag: "tag",
  selector: "tag",
};

function highlight(code: string, language: string): HighlightSpan[][] {
  const rows: HighlightSpan[][] = [[]];
  let column = 0;
  let count = 0;
  function walk(node: Element | Text, inherited?: SyntaxKind) {
    if (node.type === "element") {
      const classes = node.properties.className;
      const kind = Array.isArray(classes)
        ? classes.map((name) => kinds[String(name)]).find(Boolean) || inherited
        : inherited;
      for (const child of node.children) {
        if (child.type === "element" || child.type === "text")
          walk(child, kind);
      }
      return;
    }
    const parts = node.value.split("\n");
    for (let i = 0; i < parts.length; i++) {
      if (i) {
        rows.push([]);
        column = 0;
      }
      const end = column + parts[i].length;
      if (inherited && end > column) {
        if (++count > 50000) throw new Error("Syntax token limit");
        rows[rows.length - 1].push({ start: column, end, kind: inherited });
      }
      column = end;
    }
  }
  for (const node of refractor.highlight(code, language).children) {
    if (node.type === "element" || node.type === "text") walk(node);
  }
  return rows;
}

// A disposable worker keeps slow grammars off the UI thread.
export function highlightRequest(request: SyntaxRequest): SyntaxResult {
  const language = languageFor(request.path);
  if (!language) return { lines: [] };
  const grammar = language;
  const size =
    request.kind === "source"
      ? request.code.length
      : request.lines.reduce((n, line) => n + line.text.length + 1, 0);
  if (size > 250000) return { lines: [], limited: true };
  if (request.kind === "source")
    return { lines: highlight(request.code, language) };

  const lines = request.lines;
  const rows: HighlightSpan[][] = lines.map(() => []);
  let group: number[] = [];
  function flush() {
    // Base and head need independent lexical state.
    for (const side of ["base", "head"]) {
      const indices = group.filter(
        (i) => lines[i].kind !== (side === "base" ? "add" : "delete"),
      );
      const code = indices.map((i) => lines[i].text).join("\n");
      const colors = highlight(code, grammar);
      indices.forEach((index, i) => {
        if (side === "head" || lines[index].kind === "delete")
          rows[index] = colors[i] || [];
      });
    }
    group = [];
  }
  request.lines.forEach((line, i) => {
    if (line.kind === "hunk") flush();
    else if (line.kind !== "notice") group.push(i);
  });
  flush();
  return { lines: rows };
}
