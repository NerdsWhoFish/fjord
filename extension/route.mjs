// Bounded owner/repo repetition can exceed Chrome's compiled-regex budget.
export const changesPattern =
  /^https:\/\/github\.com\/([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)\/([A-Za-z0-9_.-]+)\/pull\/([1-9][0-9]{0,14})\/changes\/?(?:[?#].*)?$/;

export function fjordURL(raw) {
  if (typeof raw !== "string") return undefined;
  let url;
  try {
    url = new URL(raw);
  } catch {
    return undefined;
  }
  if (url.origin !== "https://github.com" || url.username || url.password)
    return undefined;
  const match = changesPattern.exec(url.href);
  if (!match) return undefined;
  return `https://fjord.stout.zone/${match[1]}/${match[2]}/${match[3]}`;
}

export async function redirectHistory(details, tabs) {
  if (
    details.frameId !== 0 ||
    details.tabId < 0 ||
    details.documentLifecycle !== "active"
  )
    return false;
  const target = fjordURL(details.url);
  if (!target) return false;
  try {
    const tab = await tabs.get(details.tabId);
    // A newer navigation must win over a delayed history event.
    if (
      tab.url !== details.url ||
      (tab.pendingUrl && tab.pendingUrl !== details.url)
    )
      return false;
    await tabs.update(details.tabId, { url: target });
    return true;
  } catch {
    return false;
  }
}
