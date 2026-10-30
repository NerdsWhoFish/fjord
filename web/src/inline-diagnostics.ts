import type {
  NavigationCapabilities,
  NavigationResult,
  ReviewItem,
  Selection,
} from "./types";

export type DiagnosticState =
  | { status: "loading" }
  | { status: "unsupported"; message: string }
  | { status: "error"; message: string }
  | { status: "ready"; items: ReviewItem[]; message: string; pending: boolean };

export function diagnosticKey(
  selection: Pick<Selection, "path" | "sha">,
): string {
  return `${selection.sha}\0${selection.path}`;
}

export class DiagnosticCache {
  private entries = new Map<string, Promise<DiagnosticState>>();
  private waiting: (() => void)[] = [];
  private running = 0;

  constructor(
    private capabilities: (
      selection: Selection,
    ) => Promise<NavigationCapabilities>,
    private query: (selection: Selection) => Promise<NavigationResult>,
  ) {}

  invalidate(selection: Selection) {
    this.entries.delete(diagnosticKey(selection));
  }

  load(selection: Selection, retry = false): Promise<DiagnosticState> {
    const key = diagnosticKey(selection);
    if (retry) this.entries.delete(key);
    const cached = this.entries.get(key);
    if (cached) return cached;
    if (this.waiting.length >= 128)
      return Promise.resolve({
        status: "error",
        message:
          "Diagnostic requests are busy. Retry this file after loading completes.",
      });
    const pending = new Promise<DiagnosticState>((resolve) => {
      this.waiting.push(() => {
        this.running++;
        void this.inspect(selection)
          .then(resolve)
          .finally(() => {
            this.running--;
            this.drain();
          });
      });
    });
    this.entries.set(key, pending);
    // Bound retained reports even when a PR contains thousands of files.
    if (this.entries.size > 128)
      this.entries.delete(this.entries.keys().next().value!);
    this.drain();
    return pending;
  }

  private drain() {
    while (this.running < 2 && this.waiting.length) this.waiting.shift()!();
  }

  private async inspect(selection: Selection): Promise<DiagnosticState> {
    try {
      const capabilities = await this.capabilities(selection);
      if (!capabilities.methods.includes("diagnostics"))
        return {
          status: "unsupported",
          message:
            capabilities.message ||
            "Diagnostics are unavailable for this file.",
        };
      const result = await this.query(selection);
      const items = (result.items || []).filter(
        (item) => item.location?.path === selection.path,
      );
      const message = result.message || "";
      return {
        status: "ready",
        items,
        message,
        pending:
          !items.length && /not published|pending|unchanged/i.test(message),
      };
    } catch (cause) {
      return {
        status: "error",
        message:
          cause instanceof Error
            ? cause.message
            : "Could not load diagnostics.",
      };
    }
  }
}

export function diagnosticsForLine(
  items: ReviewItem[],
  path: string,
  line: number,
): ReviewItem[] {
  return items.filter(
    ({ location }) =>
      location?.path === path &&
      line >= location.line &&
      (line < location.endLine ||
        (line === location.endLine &&
          (location.endCharacter > 0 || location.line === location.endLine))),
  );
}

export function diagnosticSeverity(items: ReviewItem[]): string {
  return (
    ["error", "warning", "information", "hint", "diagnostic"].find((kind) =>
      items.some((item) => item.kind === kind),
    ) || "diagnostic"
  );
}
