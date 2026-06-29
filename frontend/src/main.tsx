import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { initAppBridge } from "./lib/appBridge";

// Initialize App Bridge BEFORE React renders. This matters because importing
// ./lib/appBridge captures the Shopify `host` query param at module-load time
// (before React Router's "/" → "/products" redirect strips the query string),
// and calling initAppBridge() here creates the App Bridge instance up front so
// the very first /api call already has a fresh session token.
initAppBridge();

const rootEl = document.getElementById("root");
if (!rootEl) {
  throw new Error("Root element #root not found");
}

createRoot(rootEl).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
