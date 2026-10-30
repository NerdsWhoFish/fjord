import type { Method } from "./types";

export const methodLabels: Record<Method, string> = {
  goTo: "Go to",
  definition: "Definition",
  references: "References",
  implementation: "Implementation",
  hover: "Type information",
  diagnostics: "Diagnostics",
  incomingCalls: "Incoming calls",
  outgoingCalls: "Outgoing calls",
  supertypes: "Supertypes",
  subtypes: "Subtypes",
  documentSymbols: "File outline",
  workspaceSymbols: "Workspace symbols",
  documentHighlights: "Highlight occurrences",
  inlayHints: "Inlay hints",
  codeActions: "Code actions",
  rename: "Preview rename",
};

const fileMethods: Method[] = [
  "diagnostics",
  "documentSymbols",
  "workspaceSymbols",
  "inlayHints",
];
const symbolMethods: Method[] = [
  "implementation",
  "hover",
  "incomingCalls",
  "outgoingCalls",
  "supertypes",
  "subtypes",
  "documentHighlights",
  "codeActions",
  "rename",
  ...fileMethods,
];

export function availableActions(methods: Method[], file = false): Method[] {
  const supported = new Set(methods);
  if (file) return fileMethods.filter((method) => supported.has(method));
  const actions = symbolMethods.filter((method) => supported.has(method));
  if (supported.has("definition") || supported.has("references"))
    actions.unshift("goTo");
  return actions;
}
