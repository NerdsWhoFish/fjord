import { redirectHistory } from "./route.mjs";

chrome.webNavigation.onHistoryStateUpdated.addListener(
  (details) => {
    void redirectHistory(details, chrome.tabs);
  },
  { url: [{ schemes: ["https"], hostEquals: "github.com" }] },
);
