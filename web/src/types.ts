export type Side = "LEFT" | "RIGHT";
export type Method = "definition" | "implementation" | "references" | "hover";
export interface NavigationCapabilities {
  server: string;
  languageId: string;
  methods: Method[];
  message?: string;
}
export type ReviewEvent = "COMMENT" | "APPROVE" | "REQUEST_CHANGES";
export interface ChangedFile {
  path: string;
  previousPath?: string;
  status: string;
  additions: number;
  deletions: number;
  patch?: string;
}
export interface PullRequest {
  owner: string;
  repo: string;
  number: number;
  title: string;
  body: string;
  url: string;
  author: string;
  state: string;
  baseSHA: string;
  headSHA: string;
  baseRef: string;
  headRef: string;
  files: ChangedFile[];
}
export interface DraftComment {
  id: string;
  path: string;
  line: number;
  side: Side;
  startLine?: number;
  startSide?: Side;
  body: string;
}
export interface ThreadComment {
  id: number;
  body: string;
  path: string;
  line: number;
  side: Side;
  startLine?: number;
  startSide?: Side;
  author: string;
  url: string;
}
export interface CodeLocation {
  path: string;
  line: number;
  character: number;
  endLine: number;
  endCharacter: number;
}
export interface NavigationResult {
  locations: CodeLocation[];
  hover: string;
  message?: string;
}
export interface Selection {
  path: string;
  side: Side;
  line: number;
  startLine?: number;
  startSide?: Side;
  character: number;
  word: string;
  code: string;
  sha: string;
}
export interface SourceView {
  path: string;
  sha: string;
  content: string;
  highlight?: number;
  workspace?: string;
}
export interface Config {
  aiEnabled: boolean;
  telemetryEnabled: boolean;
}
