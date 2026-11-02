import { expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { CodePanel } from "./CodePanel";
import { demoPR } from "./demo";
import type { ChangedFile } from "./types";

it("keeps read-only reconstructed code navigable without comment anchors", () => {
  const file: ChangedFile = {
    path: "renamed.go",
    previousPath: "old.go",
    status: "renamed",
    additions: 0,
    deletions: 0,
    patch: "@@ -1,1 +1,1 @@\n package main",
    reviewable: false,
  };
  const html = renderToStaticMarkup(
    <CodePanel
      pr={{ ...demoPR, files: [file] }}
      file={file}
      scrollTarget={{ path: file.path }}
      onActiveFile={() => {}}
      viewed={new Set()}
      onViewed={() => {}}
      busy={false}
      method="goTo"
      onMethod={() => {}}
      onSelect={() => {}}
      onNavigate={async () => undefined}
      onSuggest={async () => {}}
      onClearAnnotations={() => {}}
      onCapabilities={async () => ({
        server: "gopls",
        languageId: "go",
        methods: [],
      })}
      onDiagnostics={async () => ({ locations: [], hover: "" })}
      onSource={() => {}}
      onLoadContent={async () => "package main"}
      onLocation={() => {}}
      onCloseSource={() => {}}
      onComment={() => {}}
    />,
  );
  expect(html).toContain("Inspect main");
  expect(html).not.toContain("Select head line");
  expect(html).not.toContain("Select base line");
  expect(html).toContain(">1</span>");
});
