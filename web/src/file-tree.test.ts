import { describe, expect, it } from "vitest";
import {
  buildFileTree,
  fileKind,
  fileStatus,
  filterChangedFiles,
  folderPaths,
} from "./file-tree";
import type { ChangedFile } from "./types";

function changed(
  path: string,
  status = "modified",
  previousPath?: string,
): ChangedFile {
  return { path, status, previousPath, additions: 1, deletions: 1 };
}

describe("changed file hierarchy", () => {
  it("groups folders by their full path, with folders before naturally sorted files", () => {
    const files = [
      changed("README.md"),
      changed("src/main10.go"),
      changed("src/main2.go"),
      changed("src/test/helpers.go"),
      changed("test/helpers.go"),
      changed(".gitignore"),
    ];
    const original = [...files];
    const tree = buildFileTree(files);
    expect(tree.map((node) => node.path)).toEqual([
      "src",
      "test",
      ".gitignore",
      "README.md",
    ]);
    expect(folderPaths(tree)).toEqual(["src", "src/test", "test"]);
    const src = tree[0];
    expect(
      src.kind === "folder" && src.children.map((node) => node.path),
    ).toEqual(["src/test", "src/main2.go", "src/main10.go"]);
    expect(files).toEqual(original);
  });

  it("retains only matching ancestors and finds renamed files by their previous path", () => {
    const files = [
      changed("internal/transport/client.go", "renamed", "internal/api.go"),
      changed("internal/transport/server.go"),
      changed("docs/README.md"),
    ];
    const matches = filterChangedFiles(
      files,
      "API.GO",
      "all",
      false,
      new Set(),
    );
    expect(matches.map((file) => file.path)).toEqual([
      "internal/transport/client.go",
    ]);
    expect(folderPaths(buildFileTree(matches))).toEqual([
      "internal",
      "internal/transport",
    ]);
  });

  it("combines path, change-status, and unviewed filters without losing source data", () => {
    const files = [
      changed("src/one.go", "added"),
      changed("src/two.go", "added"),
      changed("src/three.go", "removed"),
      changed("docs/notes.md", "added"),
    ];
    expect(
      filterChangedFiles(
        files,
        " src/ ",
        "added",
        true,
        new Set(["src/one.go"]),
      ),
    ).toEqual([files[1]]);
    expect(
      filterChangedFiles(
        files,
        "",
        "all",
        false,
        new Set(files.map((file) => file.path)),
      ),
    ).toEqual(files);
    expect(
      filterChangedFiles(files, "missing", "all", false, new Set()),
    ).toEqual([]);
  });

  it("normalizes GitHub change statuses and distinguishes useful file types", () => {
    expect(fileStatus("changed")).toBe("modified");
    expect(fileStatus("renamed")).toBe("renamed");
    expect(fileStatus("copied")).toBe("copied");
    expect(fileStatus("unknown")).toBe("other");
    expect(fileKind("src/main_test.go")).toBe("Go test");
    expect(fileKind("admin/main.tf")).toBe("OpenTofu configuration");
    expect(fileKind("go.mod")).toBe("Go module");
  });
});
