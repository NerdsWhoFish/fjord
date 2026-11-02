import type { DiffLine } from "./diff";

export type SyntaxKind =
  | "comment"
  | "keyword"
  | "string"
  | "number"
  | "function"
  | "type"
  | "property"
  | "operator"
  | "punctuation"
  | "tag";
export interface HighlightSpan {
  start: number;
  end: number;
  kind: SyntaxKind;
}
export type SyntaxRequest =
  | { kind: "source"; path: string; code: string }
  | { kind: "diff"; path: string; lines: DiffLine[] };
export interface SyntaxResult {
  lines: HighlightSpan[][];
  limited?: boolean;
}
