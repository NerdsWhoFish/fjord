import type { ChangedFile } from "./types";

export type FileStatus =
  "added" | "modified" | "removed" | "renamed" | "copied" | "other";

export type StatusFilter = "all" | FileStatus;

export interface FileNode {
  kind: "file";
  name: string;
  path: string;
  file: ChangedFile;
}

export interface FolderNode {
  kind: "folder";
  name: string;
  path: string;
  children: FileTreeNode[];
}

export type FileTreeNode = FileNode | FolderNode;

export const statusLabels: Record<FileStatus, string> = {
  added: "Added",
  modified: "Modified",
  removed: "Removed",
  renamed: "Renamed",
  copied: "Copied",
  other: "Other changes",
};

export function fileStatus(status: string): FileStatus {
  switch (status) {
    case "added":
    case "removed":
    case "renamed":
    case "copied":
      return status;
    case "modified":
    case "changed":
      return "modified";
    default:
      return "other";
  }
}

export function filterChangedFiles(
  files: ChangedFile[],
  query: string,
  status: StatusFilter,
  unviewed: boolean,
  viewed: ReadonlySet<string>,
): ChangedFile[] {
  const needle = query.trim().toLowerCase();
  return files.filter(
    (file) =>
      (!needle ||
        file.path.toLowerCase().includes(needle) ||
        file.previousPath?.toLowerCase().includes(needle)) &&
      (status === "all" || fileStatus(file.status) === status) &&
      (!unviewed || !viewed.has(file.path)),
  );
}

export function buildFileTree(files: ChangedFile[]): FileTreeNode[] {
  const root: FileTreeNode[] = [];
  const folders = new Map<string, FolderNode>();
  for (const file of files) {
    const parts = file.path.split("/");
    let children = root;
    for (let index = 0; index < parts.length - 1; index++) {
      const path = parts.slice(0, index + 1).join("/");
      let folder = folders.get(path);
      if (!folder) {
        folder = { kind: "folder", name: parts[index], path, children: [] };
        folders.set(path, folder);
        children.push(folder);
      }
      children = folder.children;
    }
    children.push({
      kind: "file",
      name: parts[parts.length - 1],
      path: file.path,
      file,
    });
  }
  function sort(nodes: FileTreeNode[]) {
    nodes.sort((a, b) => {
      if (a.kind !== b.kind) return a.kind === "folder" ? -1 : 1;
      return (
        a.name.localeCompare(b.name, "en", {
          numeric: true,
          sensitivity: "base",
        }) || a.name.localeCompare(b.name, "en")
      );
    });
    for (const node of nodes) if (node.kind === "folder") sort(node.children);
  }
  sort(root);
  return root;
}

export function folderPaths(nodes: FileTreeNode[]): string[] {
  return nodes.flatMap((node) =>
    node.kind === "folder" ? [node.path, ...folderPaths(node.children)] : [],
  );
}

export function fileKind(path: string): string {
  const name = path.split("/").at(-1) || path;
  if (name.endsWith("_test.go")) return "Go test";
  if (name === "Dockerfile" || name.startsWith("Dockerfile."))
    return "Container configuration";
  if (name === "go.mod" || name === "go.sum") return "Go module";
  const kinds: Record<string, string> = {
    go: "Go source",
    ts: "TypeScript source",
    tsx: "TypeScript component",
    js: "JavaScript source",
    jsx: "JavaScript component",
    py: "Python source",
    rs: "Rust source",
    swift: "Swift source",
    tf: "OpenTofu configuration",
    hcl: "HCL configuration",
    yaml: "YAML configuration",
    yml: "YAML configuration",
    json: "JSON configuration",
    toml: "TOML configuration",
    md: "Markdown document",
    css: "Stylesheet",
    html: "HTML document",
    sh: "Shell script",
    sql: "SQL source",
  };
  return kinds[name.split(".").at(-1)?.toLowerCase() || ""] || "File";
}
