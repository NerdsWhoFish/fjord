import {
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
} from "react";
import { api } from "./api";
import { parsePRRoute, prPath } from "./pr-route";
import { PRLoading } from "./PRLoading";
import { reviewRedirect } from "./review-redirect";
import { OutlinePanel } from "./OutlinePanel";
import { WorkspaceSearch } from "./WorkspaceSearch";
import { MCPResults } from "./MCPResults";
import { useReviewSession } from "./useReviewSession";
import type { ActionContext, SessionAction } from "./session-client";
import "./workspace-insights.css";
import { CodePanel } from "./CodePanel";
import { FileTree } from "./FileTree";
import { SettingsMenu } from "./SettingsMenu";
import { useDiffMode } from "./diff-preference";
import { demoBaseSource, demoPR, demoSource, demoThreads } from "./demo";
import { emptyDraft, readDraft, ReviewPanel, writeDraft } from "./ReviewPanel";
import type { ReviewDraft } from "./ReviewPanel";
import { captureError, startTelemetry } from "./telemetry";
import { combinedNavigation } from "./combined-navigation";
import { codeActionSuggestions } from "./code-action-suggestions";
import { remainingDraft, submissionBody } from "./review-draft";
import type {
  CodeLocation,
  ConversationEntry,
  FileDiff,
  ChangedFile,
  Config,
  CodeAnnotations,
  Method,
  NavigationResult,
  NavigationCapabilities,
  PullRequest,
  QueryOptions,
  ReviewAction,
  ReviewEvent,
  Selection,
  SourceView,
  ThreadComment,
} from "./types";

function Brand() {
  return (
    <a href="/" className="brand" aria-label="Fjord home">
      <span className="brand-mark" aria-hidden="true">
        <i />
        <i />
        <i />
      </span>
      <span>
        fjord<span className="brand-dot">.</span>
      </span>
    </a>
  );
}

export function App() {
  const [diffMode, setDiffMode] = useDiffMode();
  const [initialRoute] = useState(() =>
    parsePRRoute({
      pathname: location.pathname,
      search: location.search,
      hash: "",
    }),
  );
  const [url, setURL] = useState(
    initialRoute.kind === "pr" ? initialRoute.githubURL : "",
  );
  const [pr, setPR] = useState<PullRequest>();
  const [demo, setDemo] = useState(false);
  const [loading, setLoading] = useState(initialRoute.kind === "pr");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [selectedFile, setSelectedFile] = useState("");
  const [scrollTarget, setScrollTarget] = useState({ path: "" });
  const [selection, setSelection] = useState<Selection>();
  const [source, setSource] = useState<SourceView>();
  const [annotations, setAnnotations] = useState<CodeAnnotations>();
  const [method, setMethod] = useState<Method>("definition");
  const [navigating, setNavigating] = useState(false);
  const [navWorkspace, setNavWorkspace] = useState("");
  const [navSHA, setNavSHA] = useState("");
  const [draft, setDraft] = useState<ReviewDraft>(emptyDraft);
  const draftRef = useRef(draft);
  draftRef.current = draft;
  const [pendingComment, setPendingComment] = useState<Selection>();
  const [threads, setThreads] = useState<ThreadComment[]>([]);
  const [conversation, setConversation] = useState<ConversationEntry[]>([]);
  const [threadsLoading, setThreadsLoading] = useState(false);
  const [threadsError, setThreadsError] = useState("");
  const [reviewOpen, setReviewOpen] = useState(false);
  const [assistantResult, setAssistantResult] = useState<{
    id: string;
    selection: Selection;
    method: Method;
    result: NavigationResult;
  }>();
  const [mobileTab, setMobileTab] = useState<"code" | "review">("code");
  const [viewed, setViewed] = useState<Set<string>>(new Set());
  const [filter, setFilter] = useState("");
  const epoch = useRef(0);
  const prEpoch = useRef(0);
  const threadEpoch = useRef(0);
  const loadEpoch = useRef(0);
  const loadController = useRef<AbortController | undefined>(undefined);
  const routeAddress = useRef("");
  const workspaces = useRef(new Map<string, { id: string; expires: number }>());
  const workspaceRequests = useRef(new Map<string, Promise<string>>());
  const diffRequests = useRef(new Map<string, Promise<FileDiff>>());
  const capabilityRequests = useRef(
    new Map<
      string,
      { promise: Promise<NavigationCapabilities>; expires: number }
    >(),
  );
  const outlineRequests = useRef(new Map<string, Promise<NavigationResult>>());
  const file = pr?.files.find((f) => f.path === selectedFile);
  const inspectionSelection = useMemo<Selection | undefined>(() => {
    if (!pr || (!source && !file)) return;
    const base = source
      ? source.sha === pr.baseSHA
      : file?.status === "removed";
    return {
      path:
        source?.path || (base ? file!.previousPath || file!.path : file!.path),
      sha: source?.sha || (base ? pr.baseSHA : pr.headSHA),
      side: base ? "LEFT" : "RIGHT",
      line: 1,
      character: 0,
      word: "",
      code: "",
    };
  }, [pr, file, source]);

  const ensureWorkspace = useCallback(
    async (sha: string): Promise<string> => {
      if (!pr) throw new Error("Open a pull request before following code.");
      if (demo) return "demo";
      const existing = workspaces.current.get(sha);
      if (existing && existing.expires > Date.now()) return existing.id;
      const pending = workspaceRequests.current.get(sha);
      if (pending) return pending;
      const currentPR = prEpoch.current;
      const promise = api<{ id: string }>("workspace", {
        owner: pr.owner,
        repo: pr.repo,
        sha,
      })
        .then(({ id }) => {
          if (prEpoch.current === currentPR)
            workspaces.current.set(sha, {
              id,
              expires: Date.now() + 15 * 60 * 1000,
            });
          return id;
        })
        .finally(() => {
          if (workspaceRequests.current.get(sha) === promise)
            workspaceRequests.current.delete(sha);
        });
      workspaceRequests.current.set(sha, promise);
      return promise;
    },
    [pr, demo],
  );

  const navigationCapabilities = useCallback(
    (selected: Selection): Promise<NavigationCapabilities> => {
      if (demo)
        return Promise.resolve({
          server: "Sample workspace",
          languageId: "go",
          methods: ["definition", "implementation", "references", "hover"],
        });
      const key = `${selected.sha}:${selected.path}`;
      const cached = capabilityRequests.current.get(key);
      if (cached && cached.expires > Date.now()) return cached.promise;
      const currentPR = prEpoch.current;
      const promise = ensureWorkspace(selected.sha)
        .then((id) =>
          api<NavigationCapabilities>("navigation/capabilities", {
            id,
            path: selected.path,
          }),
        )
        .catch((error) => {
          if (prEpoch.current === currentPR) {
            capabilityRequests.current.delete(key);
            workspaces.current.delete(selected.sha);
          }
          throw error;
        });
      capabilityRequests.current.set(key, {
        promise,
        expires: Date.now() + 15 * 60 * 1000,
      });
      return promise;
    },
    [demo, ensureWorkspace],
  );

  const inspectCode = useCallback(
    async (
      selected: Selection,
      action: "documentSymbols" | "workspaceSymbols" | "diagnostics",
      options?: QueryOptions,
    ): Promise<NavigationResult> => {
      if (demo)
        return {
          locations: [],
          hover: "",
          items: [],
          message: "Open a real pull request for language-server insights.",
        };
      const key = `${selected.sha}:${selected.path}`;
      if (action === "documentSymbols") {
        const cached = outlineRequests.current.get(key);
        if (cached) return cached;
      }
      const currentPR = prEpoch.current;
      const promise = (async () => {
        const capabilities = await navigationCapabilities(selected);
        if (!capabilities.methods.includes(action))
          return {
            locations: [],
            hover: "",
            message:
              capabilities.message ||
              "This language server does not support this feature.",
          };
        const id = await ensureWorkspace(selected.sha);
        return api<NavigationResult>("navigate", {
          id,
          path: selected.path,
          method: action,
          line: selected.line - 1,
          character: selected.character,
          options,
        });
      })().catch((error) => {
        if (prEpoch.current === currentPR) outlineRequests.current.delete(key);
        captureError(error, "navigate");
        throw error;
      });
      if (action === "documentSymbols") {
        if (outlineRequests.current.size >= 64)
          outlineRequests.current.delete(
            outlineRequests.current.keys().next().value!,
          );
        outlineRequests.current.set(key, promise);
      }
      return promise;
    },
    [demo, navigationCapabilities, ensureWorkspace],
  );
  const loadDiagnostics = useCallback(
    (selected: Selection) => inspectCode(selected, "diagnostics"),
    [inspectCode],
  );
  function startReview(event = draft.event) {
    changeDraft({ ...draft, event });
    setReviewOpen(true);
  }
  async function showInspectionLocation(
    location: CodeLocation,
    selected: Selection,
  ) {
    const requestEpoch = ++epoch.current;
    try {
      const workspace = await ensureWorkspace(selected.sha);
      if (requestEpoch !== epoch.current) return;
      setMobileTab("code");
      await showLocation(location, workspace, selected.sha, requestEpoch);
    } catch (error) {
      captureError(error, "navigate");
      if (requestEpoch === epoch.current)
        setError(
          error instanceof Error ? error.message : "Could not open the symbol.",
        );
    }
  }

  function changeDraft(next: ReviewDraft) {
    draftRef.current = next;
    setDraft(next);
    if (pr && !writeDraft(pr, demo, next))
      setNotice(
        "Browser storage is unavailable. Keep this tab open to preserve your draft.",
      );
  }
  function chooseFile(path: string) {
    epoch.current++;
    setSelectedFile(path);
    setScrollTarget({ path });
    setSource(undefined);
    setSelection(undefined);
    setNavigating(false);
    setMobileTab("code");
  }
  function selectSymbol(next: Selection) {
    epoch.current++;
    setNavigating(false);
    setSelection(next);
  }
  function acceptPR(next: PullRequest, isDemo: boolean) {
    epoch.current++;
    prEpoch.current++;
    threadEpoch.current++;
    setPR(next);
    setDemo(isDemo);
    setSelectedFile(next.files[0]?.path || "");
    setScrollTarget({ path: next.files[0]?.path || "" });
    setSource(undefined);
    setAnnotations(undefined);
    setAssistantResult(undefined);
    setReviewOpen(false);
    setPendingComment(undefined);
    setSelection(undefined);
    const restored = readDraft(next, isDemo);
    draftRef.current = restored;
    setDraft(restored);
    setViewed(new Set());
    setThreads([]);
    setConversation([]);
    setThreadsLoading(false);
    setThreadsError("");
    setNotice("");
    setError("");
    setFilter("");
    workspaces.current.clear();
    workspaceRequests.current.clear();
    diffRequests.current.clear();
    capabilityRequests.current.clear();
    outlineRequests.current.clear();
    setMobileTab("code");
  }
  async function reloadThreads(next = pr, isDemo = demo) {
    if (!next) return;
    if (isDemo) {
      setThreads(demoThreads);
      setConversation([]);
      setThreadsLoading(false);
      return;
    }
    const currentPR = prEpoch.current;
    const currentRequest = ++threadEpoch.current;
    setThreadsError("");
    setThreadsLoading(true);
    try {
      const result = await api<{
        comments: ThreadComment[];
        conversation: ConversationEntry[];
      }>("comments", {
        owner: next.owner,
        repo: next.repo,
        number: next.number,
      });
      if (
        prEpoch.current === currentPR &&
        threadEpoch.current === currentRequest
      ) {
        setThreads(result.comments || []);
        setConversation(result.conversation || []);
      }
    } catch (error) {
      if (
        prEpoch.current === currentPR &&
        threadEpoch.current === currentRequest
      )
        setThreadsError(
          error instanceof Error
            ? error.message
            : "Could not load the conversation.",
        );
    } finally {
      if (
        prEpoch.current === currentPR &&
        threadEpoch.current === currentRequest
      )
        setThreadsLoading(false);
    }
  }
  function leavePR() {
    loadEpoch.current++;
    loadController.current?.abort();
    epoch.current++;
    prEpoch.current++;
    threadEpoch.current++;
    setPR(undefined);
    setAssistantResult(undefined);
    setSource(undefined);
    setSelection(undefined);
    setReviewOpen(false);
    setPendingComment(undefined);
    setNavigating(false);
    setLoading(false);
    setNotice("");
    setError("");
  }
  async function loadPR(rawURL: string, history: "push" | "replace") {
    leavePR();
    const requestEpoch = ++loadEpoch.current;
    const controller = new AbortController();
    loadController.current = controller;
    setURL(rawURL);
    setError("");
    setLoading(true);
    try {
      const next = await api<PullRequest>(
        "pr",
        { url: rawURL.trim() },
        controller.signal,
      );
      if (requestEpoch !== loadEpoch.current) return;
      acceptPR(next, false);
      const path = prPath(next);
      if (history === "push" && location.pathname + location.search !== path)
        window.history.pushState(null, "", path);
      else window.history.replaceState(null, "", path);
      routeAddress.current = location.pathname + location.search;
      void reloadThreads(next, false);
      const currentPR = prEpoch.current;
      api<Config>("config")
        .then((nextConfig) => {
          if (prEpoch.current !== currentPR) return;
          if (nextConfig.telemetryEnabled) startTelemetry();
        })
        .catch(() => {
          if (prEpoch.current === currentPR)
            setNotice(
              "Telemetry configuration could not be loaded. Review and navigation remain available.",
            );
        });
    } catch (error) {
      if (controller.signal.aborted || requestEpoch !== loadEpoch.current)
        return;
      captureError(error, "pr.load");
      setError(
        error instanceof Error
          ? error.message
          : "Could not open this pull request.",
      );
    } finally {
      if (requestEpoch === loadEpoch.current) setLoading(false);
    }
  }
  function openPR(event: React.FormEvent) {
    event.preventDefault();
    void loadPR(url, "push");
  }
  const followRoute = useEffectEvent(() => {
    // Fragment links are local keyboard/navigation affordances, not PR identities.
    const route = parsePRRoute({
      pathname: location.pathname,
      search: location.search,
      hash: "",
    });
    if (route.kind === "pr") {
      void loadPR(route.githubURL, "replace");
    } else {
      leavePR();
      setURL("");
      if (route.kind === "error") setError(route.message);
    }
  });
  useEffect(() => {
    routeAddress.current = location.pathname + location.search;
    followRoute();
    function popstate() {
      const next = location.pathname + location.search;
      if (next === routeAddress.current) return;
      routeAddress.current = next;
      followRoute();
    }
    window.addEventListener("popstate", popstate);
    return () => {
      loadEpoch.current++;
      loadController.current?.abort();
      window.removeEventListener("popstate", popstate);
    };
  }, []);
  function openDemo() {
    loadEpoch.current++;
    loadController.current?.abort();
    window.history.replaceState(null, "", "/");
    routeAddress.current = "/";
    acceptPR(demoPR, true);
    setThreads(demoThreads);
  }
  const loadContent = useCallback(
    async (target: ChangedFile, side: "LEFT" | "RIGHT"): Promise<string> => {
      if (!pr) throw new Error("Open a pull request before loading source.");
      const path =
        side === "LEFT" ? target.previousPath || target.path : target.path;
      const sha = side === "LEFT" ? pr.baseSHA : pr.headSHA;
      if (demo)
        return (side === "LEFT" ? demoBaseSource : demoSource)[path] || "";
      return (
        await api<{ content: string }>("content", {
          owner: pr.owner,
          repo: pr.repo,
          sha,
          path,
        })
      ).content;
    },
    [pr, demo],
  );
  const loadDiff = useCallback(
    async (target: ChangedFile): Promise<FileDiff> => {
      if (!pr) throw new Error("Open a pull request before loading its diff.");
      if (demo)
        return {
          patch: "",
          status: "unavailable",
          reason: "This sample does not include a diff for this file.",
        };
      const request = {
        owner: pr.owner,
        repo: pr.repo,
        baseSHA: pr.baseSHA,
        headSHA: pr.headSHA,
        path: target.path,
        previousPath: target.previousPath,
        status: target.status,
      };
      const key = JSON.stringify(request);
      const existing = diffRequests.current.get(key);
      if (existing) return existing;
      const currentPR = prEpoch.current;
      const pending = api<FileDiff>("diff", request).then((result) => {
        if (prEpoch.current !== currentPR)
          throw new Error("The pull request changed while loading its diff.");
        return result;
      });
      diffRequests.current.set(key, pending);
      try {
        const result = await pending;
        if (
          result.status === "unavailable" &&
          diffRequests.current.get(key) === pending
        )
          diffRequests.current.delete(key);
        return result;
      } catch (error) {
        if (diffRequests.current.get(key) === pending)
          diffRequests.current.delete(key);
        throw error;
      }
    },
    [pr, demo],
  );
  async function openSource(side: "LEFT" | "RIGHT", target = file) {
    if (!pr || !target) return;
    const requestEpoch = ++epoch.current;
    setNavigating(true);
    setError("");
    const path =
      side === "LEFT" ? target.previousPath || target.path : target.path;
    const sha = side === "LEFT" ? pr.baseSHA : pr.headSHA;
    try {
      const content = await loadContent(target, side);
      if (epoch.current === requestEpoch) setSource({ path, sha, content });
    } catch (error) {
      if (epoch.current === requestEpoch)
        setError(
          error instanceof Error ? error.message : "Source is unavailable.",
        );
    } finally {
      if (epoch.current === requestEpoch) setNavigating(false);
    }
  }
  async function showLocation(
    location: CodeLocation,
    workspace = navWorkspace,
    sha = navSHA,
    requestEpoch = ++epoch.current,
  ) {
    setNavigating(true);
    try {
      const result = demo
        ? {
            content:
              (sha === pr?.baseSHA ? demoBaseSource : demoSource)[
                location.path
              ] || "",
          }
        : await api<{ content: string }>("workspace/file", {
            id: workspace,
            path: location.path,
          });
      if (epoch.current === requestEpoch)
        setSource({
          path: location.path,
          sha,
          content: result.content,
          highlight: location.line,
          workspace,
        });
    } catch (error) {
      captureError(error, "navigate");
      if (epoch.current === requestEpoch)
        setError(
          error instanceof Error
            ? error.message
            : "Could not load navigation target.",
        );
    } finally {
      if (epoch.current === requestEpoch) setNavigating(false);
    }
  }
  async function navigate(
    nextSelection = selection,
    nextMethod = method,
    options?: QueryOptions,
  ) {
    if (!pr || !nextSelection) return;
    setSelection(nextSelection);
    setMethod(nextMethod);
    const requestEpoch = ++epoch.current;
    setNavigating(true);
    setError("");
    try {
      const workspace = await ensureWorkspace(nextSelection.sha);
      const demoSymbol: Record<string, [string, number]> = {
        backoff: ["internal/harbor/backoff.go", 5],
        Connect: ["internal/harbor/connect.go", 8],
        Connector: ["internal/harbor/connect.go", 4],
        Open: ["internal/harbor/connect.go", 5],
      };
      const target = demoSymbol[nextSelection.word];
      const query = async (action: Method): Promise<NavigationResult> =>
        demo
          ? {
              locations:
                action === "hover" || !target
                  ? []
                  : [
                      {
                        path: target[0],
                        line: target[1],
                        character: 0,
                        endLine: target[1],
                        endCharacter: nextSelection.word.length,
                      },
                    ],
              hover:
                action === "hover"
                  ? `Illustrative type information for ${nextSelection.word}. Open a real PR for language-server results.`
                  : "",
            }
          : await api<NavigationResult>("navigate", {
              id: workspace,
              path: nextSelection.path,
              method: action,
              line: nextSelection.line - 1,
              character: nextSelection.character,
              options,
            });
      const result =
        nextMethod === "goTo"
          ? await combinedNavigation(
              (await navigationCapabilities(nextSelection)).methods,
              query,
            )
          : await query(nextMethod);
      if (epoch.current !== requestEpoch) return;
      setNavWorkspace(workspace);
      setNavSHA(nextSelection.sha);
      if (nextMethod === "documentHighlights" || nextMethod === "inlayHints") {
        setAnnotations((previous) => ({
          ...(previous?.path === nextSelection.path &&
          previous.sha === nextSelection.sha
            ? previous
            : {}),
          path: nextSelection.path,
          sha: nextSelection.sha,
          ...(nextMethod === "documentHighlights"
            ? { highlights: result.highlights || [] }
            : { hints: result.hints || [] }),
        }));
      }
      return result;
    } catch (error) {
      captureError(error, "navigate");
      if (epoch.current === requestEpoch) {
        workspaces.current.delete(nextSelection.sha);
        capabilityRequests.current.delete(
          `${nextSelection.sha}:${nextSelection.path}`,
        );
        setError(
          error instanceof Error ? error.message : "Navigation is unavailable.",
        );
      }
    } finally {
      if (epoch.current === requestEpoch) setNavigating(false);
    }
  }
  async function suggestAction(action: ReviewAction, selected: Selection) {
    if (!pr || selected.sha !== pr.headSHA || selected.side !== "RIGHT")
      throw new Error("Review suggestions must target the head revision.");
    if (action.disabledReason || !action.edits?.length)
      throw new Error(
        action.disabledReason || "This action has no editable changes.",
      );
    const currentPR = prEpoch.current;
    const paths = [...new Set(action.edits.map((edit) => edit.location.path))];
    if (
      paths.some(
        (path) =>
          !pr.files.some(
            (file) => file.path === path && file.status !== "removed",
          ),
      )
    )
      throw new Error(
        "This fix touches files outside the PR diff. Its edits can be previewed but cannot become inline review suggestions.",
      );
    const workspace = await ensureWorkspace(pr.headSHA);
    const contents = Object.fromEntries(
      await Promise.all(
        paths.map(async (path) => [
          path,
          demo
            ? demoSource[path] || ""
            : (
                await api<{ content: string }>("workspace/file", {
                  id: workspace,
                  path,
                })
              ).content,
        ]),
      ),
    );
    const suggestions = codeActionSuggestions(pr.files, action.edits, contents);
    if (draftRef.current.comments.length + suggestions.length > 100)
      throw new Error(
        "This fix would exceed the review limit of 100 comments. Submit or remove existing drafts first.",
      );
    if (currentPR !== prEpoch.current)
      throw new Error(
        "The pull request changed before suggestions were prepared.",
      );
    const added = suggestions.map((suggestion) => ({
      ...suggestion,
      id: Array.from(crypto.getRandomValues(new Uint32Array(4)), (n) =>
        n.toString(16).padStart(8, "0"),
      ).join(""),
    }));
    changeDraft({
      ...draftRef.current,
      comments: [...draftRef.current.comments, ...added],
    });
    setNotice(
      `Added ${added.length} suggestion ${added.length === 1 ? "draft" : "drafts"} to your review.`,
    );
  }
  async function submitReview() {
    if (!pr || demo || pr.state !== "open")
      throw new Error("Only open GitHub pull requests can receive reviews.");
    const currentPR = prEpoch.current;
    const submitted = draftRef.current;
    try {
      await api<{ url: string }>("review", {
        owner: pr.owner,
        repo: pr.repo,
        number: pr.number,
        baseSHA: pr.baseSHA,
        headSHA: pr.headSHA,
        event: submitted.event,
        body: submitted.body,
        comments: submitted.comments.map((comment) => ({
          path: comment.path,
          line: comment.line,
          side: comment.side,
          body: submissionBody(comment),
          start_line: comment.startLine,
          start_side: comment.startSide,
        })),
      });
      if (prEpoch.current === currentPR) {
        changeDraft(remainingDraft(draftRef.current, submitted));
        const destination = reviewRedirect(
          pr,
          currentPR,
          prEpoch.current,
          demo,
        );
        if (destination) window.location.assign(destination);
      } else {
        writeDraft(pr, false, remainingDraft(readDraft(pr, false), submitted));
      }
    } catch (error) {
      captureError(error, "review.submit");
      throw error;
    }
  }
  async function reply(id: number, body: string) {
    if (!pr || demo) throw new Error("Demo replies cannot be sent.");
    const currentPR = prEpoch.current;
    try {
      await api("reply", {
        owner: pr.owner,
        repo: pr.repo,
        number: pr.number,
        commentID: id,
        body,
      });
      if (prEpoch.current === currentPR) await reloadThreads();
    } catch (error) {
      captureError(error, "reply");
      throw error;
    }
  }
  async function applySessionAction(
    action: SessionAction,
    context: ActionContext,
  ) {
    if (!pr || !context.isCurrent())
      throw new Error(
        "The review changed before this action could be applied.",
      );
    if (action.kind === "draft") {
      const comment = action.comment;
      if (!comment || comment.source !== "ai")
        throw new Error("The assistant draft is invalid.");
      if (
        draftRef.current.comments.some((existing) => existing.id === comment.id)
      )
        return;
      if (draftRef.current.comments.length >= 100)
        throw new Error("The review already contains 100 draft comments.");
      changeDraft({
        ...draftRef.current,
        comments: [...draftRef.current.comments, comment],
      });
      setNotice(
        "Your assistant added a review draft. Inspect it in the review dialog before submitting.",
      );
      return;
    }
    if (
      !action.path ||
      !action.sha ||
      !action.line ||
      ![pr.headSHA, pr.baseSHA].includes(action.sha)
    )
      throw new Error(
        "The assistant action does not identify this review's source revision.",
      );
    if (action.kind === "navigation" && (!action.method || !action.result))
      throw new Error("The assistant navigation result is missing.");
    const requestEpoch = ++epoch.current;
    setNavigating(true);
    try {
      const workspace = await ensureWorkspace(action.sha);
      if (!context.isCurrent() || requestEpoch !== epoch.current)
        throw new Error("Newer navigation superseded this assistant action.");
      const { content } = await api<{ content: string }>(
        "workspace/file",
        {
          id: workspace,
          path: action.path,
        },
        context.signal,
      );
      if (!context.isCurrent() || requestEpoch !== epoch.current)
        throw new Error("Newer navigation superseded this assistant action.");
      const selected: Selection = {
        path: action.path,
        sha: action.sha,
        side: action.sha === pr.baseSHA ? "LEFT" : "RIGHT",
        line: action.line,
        character: action.character || 0,
        word: "",
        code: content.split("\n")[action.line - 1] || "",
      };
      setSource({
        path: action.path,
        sha: action.sha,
        content,
        highlight: action.line - 1,
        workspace,
      });
      setNavWorkspace(workspace);
      setNavSHA(action.sha);
      setSelection(selected);
      if (action.kind === "navigation") {
        setAssistantResult({
          id: action.id,
          selection: selected,
          method: action.method!,
          result: action.result!,
        });
        if (
          action.method === "documentHighlights" ||
          action.method === "inlayHints"
        ) {
          setAnnotations({
            path: selected.path,
            sha: selected.sha,
            highlights: action.result!.highlights,
            hints: action.result!.hints,
          });
        }
        setMobileTab("review");
      } else {
        setAssistantResult(undefined);
        setMobileTab("code");
      }
    } finally {
      if (requestEpoch === epoch.current) setNavigating(false);
    }
  }
  const reviewSession = useReviewSession({
    pr,
    demo,
    draft,
    selection,
    getDraft: () => draftRef.current,
    onAction: applySessionAction,
  });
  const openForm = (
    <form className="open-form" onSubmit={openPR}>
      <label className="sr-only" htmlFor="pr-url">
        GitHub pull request URL
      </label>
      <input
        id="pr-url"
        type="url"
        required
        placeholder="https://github.com/owner/repo/pull/123"
        value={url}
        onChange={(e) => setURL(e.target.value)}
        disabled={loading}
      />
      <button className="primary" disabled={loading}>
        {loading ? "Opening..." : "Open pull request"}
      </button>
    </form>
  );
  if (!pr)
    return (
      <div className="welcome">
        <header>
          <Brand />
          <SettingsMenu
            diffMode={diffMode}
            onDiffMode={setDiffMode}
            session={reviewSession}
            onRetrySession={reviewSession.retry}
          />
        </header>
        {loading ? (
          <PRLoading url={url} />
        ) : (
          <main className="welcome-main">
            <div className="welcome-copy">
              <span className="eyebrow">GITHUB PR REVIEW, WITH CONTEXT</span>
              <h1>
                Follow the code.
                <br />
                <span>Find your footing.</span>
              </h1>
              <p>
                A diff tells you what changed. Fjord helps you understand what
                it means. Explore definitions, follow implementations, and bring
                your review back to GitHub.
              </p>
              {openForm}
              {error && (
                <p className="error" role="alert">
                  {error}
                </p>
              )}
              <button className="demo-link" onClick={openDemo}>
                Explore a sample review <span aria-hidden="true">&rarr;</span>
              </button>
              <p className="connection-note">
                <span className="live-dot" />
                Uses your local GitHub CLI session.
              </p>
            </div>
            <div className="welcome-landscape" aria-hidden="true">
              <div className="contour c1" />
              <div className="contour c2" />
              <div className="contour c3" />
              <div className="channel" />
              <div className="landscape-label">
                <span>BEYOND THE DIFF</span>
                <strong>
                  More context.
                  <br />
                  Better judgment.
                </strong>
                <span>
                  01 / EXPLORE &nbsp; 02 / UNDERSTAND &nbsp; 03 / REVIEW
                </span>
              </div>
            </div>
          </main>
        )}
        <footer>
          Built for people who read the code.
          <span>GitHub stays the source of truth.</span>
        </footer>
      </div>
    );

  return (
    <div className="workspace">
      <nav className="skip-links" aria-label="Skip navigation">
        <a
          href="#code-content"
          onClick={(event) => {
            event.preventDefault();
            setMobileTab("code");
            requestAnimationFrame(() =>
              document.getElementById("code-content")?.focus(),
            );
          }}
        >
          Skip to code
        </a>
        <a
          href="#review-context"
          onClick={(event) => {
            event.preventDefault();
            setMobileTab("review");
            requestAnimationFrame(() =>
              document.getElementById("review-context")?.focus(),
            );
          }}
        >
          Skip to file outline
        </a>
      </nav>
      <aside className="file-rail">
        <Brand />
        <div className="rail-rule" />
        <div className="rail-heading">
          <span>Changed files</span>
          <span className="count">{pr.files.length}</span>
        </div>
        <FileTree
          key={`${pr.owner}/${pr.repo}/${pr.number}/${pr.headSHA}`}
          files={pr.files}
          selectedFile={selectedFile}
          viewed={viewed}
          filter={filter}
          onFilter={setFilter}
          onSelect={chooseFile}
        />
        <div className="rail-bottom">
          <div className="progress-track">
            <span
              style={{
                width: `${pr.files.length ? (viewed.size / pr.files.length) * 100 : 0}%`,
              }}
            />
          </div>
          <p>
            {viewed.size} of {pr.files.length} files viewed
          </p>
          <span className="rail-caption">Every change has a story.</span>
        </div>
      </aside>
      <main className="review-workspace">
        <header className="workspace-header">
          <div>
            <div className="breadcrumb">
              {pr.owner} / {pr.repo}
              <span className={`state-badge ${demo ? "demo" : ""}`}>
                {demo ? "SAMPLE REVIEW" : pr.state}
              </span>
            </div>
            <h1>
              {pr.url ? (
                <a href={pr.url} target="_blank" rel="noreferrer">
                  {pr.title} ({pr.number})
                </a>
              ) : (
                <>
                  {pr.title} ({pr.number})
                </>
              )}
            </h1>
            <p className="branch-line">
              <strong>{pr.author}</strong> wants to merge{" "}
              <code>{pr.headRef}</code>
              <span aria-hidden="true">&rarr;</span>
              <code>{pr.baseRef}</code>
            </p>
            <div
              className="review-decision-actions"
              aria-label="Review decision"
            >
              {(["COMMENT", "APPROVE", "REQUEST_CHANGES"] as ReviewEvent[]).map(
                (event) => (
                  <button
                    key={event}
                    className={event === "APPROVE" ? "review-approve" : ""}
                    onClick={() => startReview(event)}
                  >
                    {event === "COMMENT"
                      ? "Comment"
                      : event === "APPROVE"
                        ? "Approve"
                        : "Request changes"}
                  </button>
                ),
              )}
              <button className="text-button" onClick={() => startReview()}>
                {draft.comments.length} draft{" "}
                {draft.comments.length === 1 ? "comment" : "comments"} /
                Conversation
              </button>
            </div>
          </div>
          <div className="header-actions">
            <WorkspaceSearch
              selected={inspectionSelection}
              inspect={inspectCode}
              onLocation={showInspectionLocation}
            />
            <SettingsMenu
              diffMode={diffMode}
              onDiffMode={setDiffMode}
              session={reviewSession}
              onRetrySession={reviewSession.retry}
            />
          </div>
        </header>
        {demo && (
          <div className="demo-banner">
            <strong>Sample workspace</strong>
            <span>
              Navigation uses illustrative fixtures. GitHub actions are
              disabled.
            </span>
          </div>
        )}
        {error && (
          <div className="error workspace-message" role="alert">
            {error}
            <button onClick={() => setError("")} aria-label="Dismiss error">
              x
            </button>
          </div>
        )}
        {notice && (
          <div className="notice workspace-message" role="status">
            {notice}
            <button onClick={() => setNotice("")} aria-label="Dismiss notice">
              x
            </button>
          </div>
        )}
        <div className="workspace-controls">
          <div className="mobile-files">
            <label className="sr-only" htmlFor="mobile-file">
              Changed file
            </label>
            <select
              id="mobile-file"
              value={selectedFile}
              onChange={(e) => chooseFile(e.target.value)}
            >
              {pr.files.map((f) => (
                <option key={f.path} value={f.path}>
                  {f.path}
                </option>
              ))}
            </select>
          </div>
          <span className="review-caption">THE CHANGE, IN CONTEXT</span>
          <div className="mobile-tabs">
            <button
              className={mobileTab === "code" ? "active" : ""}
              onClick={() => setMobileTab("code")}
            >
              Code
            </button>
            <button
              className={mobileTab === "review" ? "active" : ""}
              onClick={() => setMobileTab("review")}
            >
              Outline
            </button>
          </div>
        </div>
        <div className={`workspace-body mobile-${mobileTab}`}>
          <div className="code-column" id="code-content" tabIndex={-1}>
            {file ? (
              <CodePanel
                diffMode={diffMode}
                threads={threads}
                drafts={draft.comments}
                onReviewDrafts={() => setReviewOpen(true)}
                onReply={demo ? undefined : reply}
                pr={pr}
                file={file}
                scrollTarget={scrollTarget}
                onActiveFile={setSelectedFile}
                viewed={viewed}
                onViewed={(path, checked) => {
                  setViewed((current) => {
                    const next = new Set(current);
                    checked ? next.add(path) : next.delete(path);
                    return next;
                  });
                }}
                selection={selection}
                source={source}
                busy={navigating}
                method={method}
                onMethod={setMethod}
                onSelect={selectSymbol}
                onNavigate={navigate}
                onSuggest={suggestAction}
                annotations={annotations}
                onClearAnnotations={() => setAnnotations(undefined)}
                onCapabilities={navigationCapabilities}
                onDiagnostics={loadDiagnostics}
                onSource={openSource}
                onLoadContent={loadContent}
                onLoadDiff={loadDiff}
                onLocation={(location) => {
                  void showLocation(location);
                }}
                onCloseSource={() => {
                  epoch.current++;
                  setNavigating(false);
                  setSource(undefined);
                }}
                onComment={(next) => {
                  setPendingComment(next);
                }}
              />
            ) : (
              <div className="empty-message">
                <h2>No changed files</h2>
                <p>This pull request has no files to review.</p>
              </div>
            )}
          </div>
          <aside
            className="context-panel"
            id="review-context"
            tabIndex={-1}
            aria-label={
              assistantResult ? "Assistant code results" : "File outline"
            }
          >
            {assistantResult ? (
              <MCPResults
                key={assistantResult.id}
                {...assistantResult}
                onLocation={showInspectionLocation}
                onSuggest={suggestAction}
                onClearAnnotations={() => setAnnotations(undefined)}
                onClose={() => setAssistantResult(undefined)}
              />
            ) : (
              <OutlinePanel
                selected={inspectionSelection}
                currentLine={
                  selection?.path === inspectionSelection?.path &&
                  selection?.sha === inspectionSelection?.sha
                    ? selection!.line - 1
                    : source?.highlight
                }
                inspect={inspectCode}
                onLocation={showInspectionLocation}
              />
            )}
          </aside>
        </div>
        <ReviewPanel
          key={`${pr.headSHA}:${demo}`}
          pr={pr}
          demo={demo}
          draft={draft}
          onDraft={changeDraft}
          threads={threads}
          conversation={conversation}
          threadsLoading={threadsLoading}
          threadsError={threadsError}
          onReloadThreads={() => {
            void reloadThreads();
          }}
          onSubmit={submitReview}
          onReply={reply}
          pendingComment={pendingComment}
          onCloseComment={() => setPendingComment(undefined)}
          open={reviewOpen}
          onCloseReview={() => setReviewOpen(false)}
        />
      </main>
    </div>
  );
}
