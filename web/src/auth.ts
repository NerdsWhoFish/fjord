const storageKey = "fjord:github-credential";
interface Credential {
  token: string;
  id: string;
}
export interface AuthState {
  status: "signed-out" | "checking" | "ready";
  login?: string;
  error?: string;
  hasSavedCredential: boolean;
  revision: number;
}
let credential: Credential | undefined;
let hasSavedCredential = false;
let state: AuthState = {
  status: "signed-out",
  hasSavedCredential,
  revision: 0,
};
let controller = new AbortController();
const listeners = new Set<() => void>();
let initialized = false;

export const isPublicMode = () =>
  globalThis.__FJORD_CONFIG__?.publicMode === true;
export function publicTokenOriginError(): string | undefined {
  if (
    typeof location !== "undefined" &&
    (location.protocol === "https:" ||
      (location.protocol === "http:" &&
        ["localhost", "127.0.0.1", "[::1]"].includes(location.hostname)))
  )
    return;
  return "Open Fjord over HTTPS before connecting your GitHub token. HTTP is allowed only on localhost.";
}
export const authSnapshot = () => state;
export const credentialID = () => credential?.id || "signed-out";
export function subscribeAuth(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
function publish(next: Omit<AuthState, "revision" | "hasSavedCredential">) {
  state = { ...next, hasSavedCredential, revision: state.revision + 1 };
  listeners.forEach((listener) => listener());
}
function clearDrafts(preserveID?: string) {
  try {
    for (let index = sessionStorage.length - 1; index >= 0; index--) {
      const key = sessionStorage.key(index);
      if (
        key?.startsWith("fjord:public:") &&
        !key.startsWith(`fjord:public:${preserveID}:`)
      )
        sessionStorage.removeItem(key);
    }
  } catch {
    // Storage may be unavailable; unmounting still discards the active review.
  }
}
function replaceCredential(next?: Credential, preserveDraftID?: string) {
  controller.abort();
  controller = new AbortController();
  credential = next;
  clearDrafts(preserveDraftID);
}
function readCredential(): Credential | undefined {
  try {
    const parsed = JSON.parse(localStorage.getItem(storageKey) || "null");
    if (
      typeof parsed?.token === "string" &&
      /^[A-Za-z0-9_]{20,255}$/.test(parsed.token) &&
      typeof parsed?.id === "string" &&
      /^[a-f0-9-]{36}$/.test(parsed.id)
    )
      return parsed;
  } catch {
    // Browsers can prohibit storage; the form explains a failed save.
  }
}
export function initializeAuth() {
  if (initialized || !isPublicMode()) return;
  initialized = true;
  const originError = publicTokenOriginError();
  if (originError) {
    publish({ status: "signed-out", error: originError });
    return;
  }
  const saved = readCredential();
  hasSavedCredential = !!saved;
  if (saved) void connect(saved.token, saved);
  window.addEventListener("storage", (event) => {
    if (event.key !== storageKey && event.key !== null) return;
    const next = readCredential();
    hasSavedCredential = !!next;
    if (
      state.status !== "checking" &&
      state.hasSavedCredential === hasSavedCredential &&
      next?.id === credential?.id &&
      next?.token === credential?.token
    )
      return;
    replaceCredential();
    publish({ status: "signed-out" });
    if (next) void connect(next.token, next);
  });
  window.addEventListener("pageshow", (event) => {
    if (!(event as PageTransitionEvent).persisted) return;
    const saved = readCredential();
    hasSavedCredential = !!saved;
    if (saved) void connect(saved.token, saved);
    else forgetToken();
  });
}
export async function connect(token: string, saved?: Credential) {
  const originError = publicTokenOriginError();
  if (originError) {
    publish({ status: "signed-out", error: originError });
    return;
  }
  token = token.trim();
  if (!/^[A-Za-z0-9_]{20,255}$/.test(token)) {
    publish({
      status: "signed-out",
      error: "Enter a valid GitHub personal access token.",
    });
    return;
  }
  replaceCredential(undefined, saved?.id);
  publish({ status: "checking" });
  const signal = controller.signal;
  try {
    const response = await fetch("/api/auth", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
      },
      body: "{}",
      signal: AbortSignal.any([signal, AbortSignal.timeout(30_000)]),
      credentials: "omit",
      redirect: "error",
    });
    const data = await response.json();
    if (signal.aborted) return;
    if (
      !response.ok ||
      data?.authenticated !== true ||
      typeof data.login !== "string"
    ) {
      if (response.status === 401) {
        try {
          localStorage.removeItem(storageKey);
          hasSavedCredential = false;
        } catch {
          /* Already unavailable. */
        }
        clearDrafts();
      }
      publish({
        status: "signed-out",
        error:
          response.status === 401
            ? "GitHub did not accept this token. Check that it has not expired or been revoked."
            : "Could not verify your token. Try again shortly.",
      });
      return;
    }
    const next = saved || { token, id: crypto.randomUUID() };
    try {
      localStorage.setItem(storageKey, JSON.stringify(next));
      hasSavedCredential = true;
    } catch {
      publish({
        status: "signed-out",
        error:
          "Browser storage is blocked. Allow local storage for this site, then try again.",
      });
      return;
    }
    credential = next;
    publish({ status: "ready", login: data.login });
  } catch {
    if (!signal.aborted)
      publish({
        status: "signed-out",
        error: "Could not reach Fjord. Check your connection and try again.",
      });
  }
}
export function forgetToken(message?: string) {
  const secureOrigin = !publicTokenOriginError();
  const previous = credential || (secureOrigin ? readCredential() : undefined);
  replaceCredential();
  try {
    localStorage.removeItem(storageKey);
    hasSavedCredential = false;
  } catch {
    /* Already unavailable. */
  }
  publish({ status: "signed-out", error: message });
  if (previous && secureOrigin)
    void fetch("/api/auth/logout", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${previous.token}`,
      },
      body: "{}",
      credentials: "omit",
      redirect: "error",
      keepalive: true,
    }).catch(() => {});
}
export async function authenticatedFetch(
  url: string,
  init: RequestInit,
): Promise<Response> {
  if (!isPublicMode()) return fetch(url, init);
  const originError = publicTokenOriginError();
  if (originError) throw new Error(originError);
  const active = credential;
  const signal = controller.signal;
  if (!active || state.status !== "ready")
    throw new Error("Connect your GitHub token first.");
  const headers = new Headers(init.headers);
  headers.set("Authorization", `Bearer ${active.token}`);
  const response = await fetch(url, {
    ...init,
    headers,
    credentials: "omit",
    redirect: "error",
    signal: init.signal ? AbortSignal.any([init.signal, signal]) : signal,
  });
  if (signal.aborted || credential !== active)
    throw new DOMException("The GitHub connection changed.", "AbortError");
  if (response.status === 401) {
    forgetToken(
      "Your token expired or was revoked. Connect a new token to continue.",
    );
    throw new Error("Reconnect your GitHub token to continue.");
  }
  return response;
}
