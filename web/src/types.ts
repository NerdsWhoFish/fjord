export type Side = "LEFT" | "RIGHT";
export type Method =
  | "goTo"
  | "definition"
  | "implementation"
  | "references"
  | "hover"
  | "diagnostics"
  | "incomingCalls"
  | "outgoingCalls"
  | "supertypes"
  | "subtypes"
  | "documentSymbols"
  | "workspaceSymbols"
  | "documentHighlights"
  | "inlayHints"
  | "codeActions"
  | "rename";
export interface QueryOptions {
  query?: string;
  newName?: string;
}
export interface ReviewItem {
  label: string;
  detail?: string;
  kind?: string;
  location?: CodeLocation;
  children?: ReviewItem[];
}
export interface DocumentHighlight {
  location: CodeLocation;
  kind?: string;
}
export interface InlayHint {
  location: CodeLocation;
  label: string;
  kind?: string;
  paddingLeft?: boolean;
  paddingRight?: boolean;
}
export interface TextEdit {
  location: CodeLocation;
  newText: string;
  oldText?: string;
}
export interface ReviewAction {
  title: string;
  kind?: string;
  edits?: TextEdit[];
  disabledReason?: string;
}
export interface CodeAnnotations {
  path: string;
  sha: string;
  highlights?: DocumentHighlight[];
  hints?: InlayHint[];
}
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
  reviewable?: boolean;
}
export interface FileDiff {
  patch: string;
  status:
    "reconstructed" | "unchanged" | "binary" | "too_large" | "unavailable";
  reason: string;
}
export interface PullRequest {
  reviewer?: string;
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
  source?: "ai";
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
  inReplyToId?: number;
  originalLine?: number;
  originalStartLine?: number;
  commitId?: string;
  originalCommitId?: string;
  createdAt?: string;
  subjectType?: "line" | "file";
  outdated?: boolean;
}
export interface ConversationEntry {
  id: number;
  kind: "comment" | "review";
  body: string;
  author: string;
  url: string;
  createdAt: string;
  state?: string;
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
  definitions?: CodeLocation[];
  references?: CodeLocation[];
  items?: ReviewItem[];
  highlights?: DocumentHighlight[];
  hints?: InlayHint[];
  actions?: ReviewAction[];
  edits?: TextEdit[];
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
  telemetryEnabled: boolean;
}
