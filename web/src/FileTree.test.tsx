import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { FileTree } from "./FileTree";
import type { ChangedFile } from "./types";

const files: ChangedFile[] = [
  { path: "admin/main.tf", status: "added", additions: 4, deletions: 0 },
  {
    path: "admin/modules/config.tf",
    previousPath: "admin/config.tf",
    status: "renamed",
    additions: 2,
    deletions: 1,
  },
];

function render(filter = "") {
  return renderToStaticMarkup(
    <FileTree
      files={files}
      selectedFile="admin/main.tf"
      viewed={new Set(["admin/main.tf"])}
      filter={filter}
      onFilter={() => {}}
      onSelect={() => {}}
    />,
  );
}

describe("file tree accessibility", () => {
  it("renders native expanded disclosures and selected, viewed and renamed file descriptions", () => {
    const html = render();
    expect(html).toContain('aria-label="Changed files"');
    expect(html).toContain('aria-expanded="true"');
    expect(html).toContain('aria-current="true"');
    expect(html).toContain(
      "admin/main.tf, added, OpenTofu configuration, viewed",
    );
    expect(html).toContain("previously admin/config.tf");
    expect(html).toContain('class="tree-document tree-status-added"');
    expect(html).toContain('class="tree-document tree-status-renamed"');
    expect(html).not.toContain('role="tree"');
  });

  it("shows a useful empty filter result and a way to clear filters", () => {
    const html = render("nothing-matches");
    expect(html).toContain("No matching files.");
    expect(html).toContain("Clear filters");
    expect(html).toContain("0 of 2 files");
    expect(html).not.toContain('class="tree-folder"');
  });
});
