import type { ReviewDraft } from "./ReviewPanel";
import { authenticatedFetch } from "./auth";
import type {
  DraftComment,
  Method,
  NavigationResult,
  PullRequest,
} from "./types";

export interface SessionSelection {
  path: string;
  sha: string;
  line: number;
  character: number;
}
export interface SessionAction {
  id: string;
  kind: "draft" | "source" | "navigation";
  comment?: DraftComment;
  path?: string;
  sha?: string;
  line?: number;
  character?: number;
  method?: Method;
  result?: NavigationResult;
}
export interface ActionContext {
  signal: AbortSignal;
  isCurrent: () => boolean;
}
export interface SessionState {
  id?: string;
  status: "connecting" | "connected" | "retrying" | "disabled" | "disconnected";
  error?: string;
}
export interface SessionSnapshot {
  draft: ReviewDraft;
  selection?: SessionSelection;
}
export interface Acknowledgement {
  id: string;
  status: "applied" | "failed";
  message?: string;
}
export class SessionHTTPError extends Error {
  constructor(
    message: string,
    public status: number,
    public code?: string,
  ) {
    super(message);
  }
}
export type SessionTransport = <T>(
  endpoint: "open" | "sync" | "close",
  body: unknown,
  signal?: AbortSignal,
) => Promise<T>;

export const sessionTransport: SessionTransport = async <T>(
  endpoint: string,
  body: unknown,
  signal?: AbortSignal,
): Promise<T> => {
  const timeout = AbortSignal.timeout(30_000);
  const response = await authenticatedFetch(`/api/session/${endpoint}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
    keepalive: endpoint === "close",
  });
  const result = await response.json().catch(() => null);
  if (!response.ok)
    throw new SessionHTTPError(
      typeof result?.error === "string"
        ? result.error
        : "Could not synchronize the review session.",
      response.status,
      result?.code,
    );
  if (!result || typeof result !== "object")
    throw new Error("Invalid review session response.");
  return result as T;
};

export function startReviewSession({
  pr,
  snapshot,
  onAction,
  onState,
  transport = sessionTransport,
  onError,
  pollInterval = 2000,
}: {
  pr: Pick<PullRequest, "url" | "headSHA" | "baseSHA">;
  snapshot: () => SessionSnapshot;
  onAction: (action: SessionAction, context: ActionContext) => Promise<void>;
  onState: (state: SessionState) => void;
  transport?: SessionTransport;
  onError?: (error: unknown) => void;
  pollInterval?: number;
}) {
  let stopped = false;
  let disconnected = false;
  let running = false;
  let retryRequested = false;
  let failures = 0;
  let sessionID: string | undefined;
  let lastActionError: string | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const controller = new AbortController();
  const pending = new Map<string, Acknowledgement>();
  const completed = new Map<string, Acknowledgement>();
  const isCurrent = () => !stopped && !controller.signal.aborted;
  function schedule(delay: number) {
    if (stopped || disconnected) return;
    clearTimeout(timer);
    timer = setTimeout(() => {
      void poll();
    }, delay);
  }
  async function poll() {
    if (stopped || running) return;
    running = true;
    retryRequested = false;
    let nextDelay = pollInterval;
    try {
      if (!sessionID) {
        onState({ status: "connecting" });
        const opened = await transport<{ id: string; expiresAt?: string }>(
          "open",
          {
            url: pr.url,
            headSHA: pr.headSHA,
            baseSHA: pr.baseSHA,
            draft: snapshot().draft,
          },
          controller.signal,
        );
        if (typeof opened.id !== "string" || !opened.id)
          throw new Error("Review session did not return an ID.");
        if (!isCurrent()) {
          void transport("close", { id: opened.id }).catch(() => {});
          return;
        }
        sessionID = opened.id;
      }
      const acknowledgements = [...pending.values()];
      const response = await transport<{
        actions: SessionAction[];
        expiresAt?: string;
      }>(
        "sync",
        {
          id: sessionID,
          ...snapshot(),
          acknowledgements,
        },
        controller.signal,
      );
      if (!isCurrent()) return;
      if (!Array.isArray(response.actions))
        throw new Error("Invalid review action queue.");
      for (const ack of acknowledgements) {
        if (pending.get(ack.id) === ack) pending.delete(ack.id);
      }
      let actionError = lastActionError;
      let appliedNewAction = false;
      for (const action of response.actions) {
        if (!isCurrent()) return;
        if (!action || typeof action.id !== "string" || !action.id)
          throw new Error("Invalid review action.");
        const previous = completed.get(action.id);
        if (previous) {
          pending.set(action.id, previous);
          continue;
        }
        if (!appliedNewAction) actionError = undefined;
        appliedNewAction = true;
        let acknowledgement: Acknowledgement;
        try {
          await onAction(action, { signal: controller.signal, isCurrent });
          if (!isCurrent()) return;
          acknowledgement = { id: action.id, status: "applied" };
        } catch (error) {
          if (!isCurrent()) return;
          actionError =
            error instanceof Error
              ? error.message
              : "Could not apply an assistant action.";
          acknowledgement = {
            id: action.id,
            status: "failed",
            message: "The browser could not apply this action.",
          };
          onError?.(error);
        }
        completed.set(action.id, acknowledgement);
        pending.set(action.id, acknowledgement);
      }
      // Keep recent IDs even after acknowledgement to absorb repeated deliveries.
      if (completed.size > 2048) {
        for (const id of completed.keys()) {
          if (!pending.has(id)) completed.delete(id);
          if (completed.size <= 2048) break;
        }
      }
      failures = 0;
      lastActionError = actionError;
      onState({ id: sessionID, status: "connected", error: actionError });
    } catch (error) {
      if (!isCurrent()) return;
      if (
        error instanceof SessionHTTPError &&
        error.status === 410 &&
        error.code === "session_removed"
      ) {
        disconnected = true;
        sessionID = undefined;
        pending.clear();
        completed.clear();
        lastActionError = undefined;
        onState({ status: "disconnected", error: error.message });
        return;
      }
      if (
        error instanceof SessionHTTPError &&
        error.status === 404 &&
        error.code === "session_not_found"
      ) {
        sessionID = undefined;
        pending.clear();
        completed.clear();
      }
      failures++;
      nextDelay = Math.min(
        30_000,
        pollInterval * 2 ** Math.min(failures - 1, 4),
      );
      onError?.(error);
      onState({
        id: sessionID,
        status: "retrying",
        error:
          error instanceof Error
            ? error.message
            : "Review session connection interrupted.",
      });
    } finally {
      running = false;
      schedule(retryRequested ? 0 : nextDelay);
    }
  }
  void poll();
  return {
    retry() {
      disconnected = false;
      failures = 0;
      if (running) retryRequested = true;
      else schedule(0);
    },
    stop() {
      if (stopped) return;
      stopped = true;
      clearTimeout(timer);
      controller.abort();
      if (sessionID) void transport("close", { id: sessionID }).catch(() => {});
    },
  };
}

export function bindSessionLifecycle(
  target: EventTarget,
  start: () => ReturnType<typeof startReviewSession>,
) {
  let active: ReturnType<typeof startReviewSession> | undefined;
  let suspended = false;
  let disposed = false;
  const hide = () => {
    suspended = true;
    active?.stop();
    active = undefined;
  };
  const show = (event: Event) => {
    if (disposed || !suspended || !(event as PageTransitionEvent).persisted)
      return;
    suspended = false;
    active = start();
  };
  target.addEventListener("pagehide", hide);
  target.addEventListener("pageshow", show);
  active = start();
  return {
    retry() {
      active?.retry();
    },
    stop() {
      if (disposed) return;
      disposed = true;
      target.removeEventListener("pagehide", hide);
      target.removeEventListener("pageshow", show);
      active?.stop();
      active = undefined;
    },
  };
}
