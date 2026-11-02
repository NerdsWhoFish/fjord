const extensions: Record<string, string> = {
  go: "go",
  js: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  ts: "typescript",
  mts: "typescript",
  cts: "typescript",
  jsx: "jsx",
  tsx: "tsx",
  py: "python",
  sh: "bash",
  bash: "bash",
  zsh: "bash",
  json: "json",
  yaml: "yaml",
  yml: "yaml",
  tf: "hcl",
  tfvars: "hcl",
  hcl: "hcl",
  md: "markdown",
  markdown: "markdown",
  css: "css",
  html: "markup",
  htm: "markup",
  xml: "markup",
  svg: "markup",
  sql: "sql",
  rs: "rust",
  rb: "ruby",
  toml: "toml",
  java: "java",
  c: "c",
  h: "c",
  cpp: "cpp",
  cc: "cpp",
  cxx: "cpp",
  hpp: "cpp",
  cs: "csharp",
  swift: "swift",
};

export function languageFor(path: string): string | undefined {
  const name = path.split("/").at(-1)?.toLowerCase() || "";
  if (name === "dockerfile" || name.startsWith("dockerfile.")) return "docker";
  if (name === "gemfile" || name === "rakefile") return "ruby";
  return extensions[name.split(".").at(-1) || ""];
}

export function codeFencePath(language: string): string | undefined {
  const name = language.toLowerCase();
  if (name === "docker" || name === "dockerfile") return "Dockerfile";
  const alias =
    name === "terraform" || name === "tofu"
      ? "hcl"
      : name === "shell"
        ? "bash"
        : name;
  const extension = extensions[alias]
    ? alias
    : Object.keys(extensions).find((ext) => extensions[ext] === alias);
  return extension ? `preview.${extension}` : undefined;
}
