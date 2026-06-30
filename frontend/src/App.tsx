/**
 * App — root of the GPSR Compliance Engine admin (embedded in Shopify Admin).
 *
 * Shell responsibilities:
 *  - Wrap the app in Polaris AppProvider (CSS + en translations)
 *  - Render <ui-nav-menu> so App Bridge lifts navigation into the Shopify
 *    Admin left sidebar (OUTSIDE the iframe — the correct embedded-app pattern)
 *  - Provide client-side routing via React Router
 *  - Route: /products  → ProductsScreen (F6)
 *  - Route: /entities  → EntityLibraryScreen (F4)
 *  - Route: /warning-templates → WarningTemplateLibraryScreen (F4)
 *  - Route: /rules     → RulesScreen (F5)
 *  - Default route → redirect to /products
 *
 * Nav architecture note:
 *   Embedded apps MUST NOT render a navigation menu inside the iframe via
 *   Polaris Frame+Navigation — that creates a duplicate in-iframe menu that
 *   does not sync with Shopify's URL bar.  Instead, <ui-nav-menu> is a
 *   custom element provided by the CDN App Bridge script (loaded in
 *   index.html).  App Bridge detects it and hoists the <a> links into the
 *   Shopify Admin sidebar chrome automatically.
 *
 * Shared files F5 also uses: App.tsx (AppProvider wraps F5's RulesScreen),
 * src/api/client.ts (shared fetch wrapper), src/api/types.ts (shared types).
 * F5 should NOT modify App.tsx's AppProvider setup — only add new <Route>
 * entries for rules.
 */

import "@shopify/polaris/build/esm/styles.css";
import { AppProvider } from "@shopify/polaris";
import enTranslations from "@shopify/polaris/locales/en.json";
import {
  BrowserRouter,
  Routes,
  Route,
  Navigate,
  useLocation,
} from "react-router-dom";

// ---------------------------------------------------------------------------
// TypeScript: declare the ui-nav-menu custom element so JSX is valid.
// The element is provided at runtime by the CDN App Bridge script; we only
// need the type declaration so the TypeScript compiler accepts the JSX tag.
// ---------------------------------------------------------------------------
declare global {
  // eslint-disable-next-line @typescript-eslint/no-namespace
  namespace JSX {
    interface IntrinsicElements {
      // Minimal declaration — App Bridge renders the element; we only set
      // standard HTML attributes (children, className, id, etc.).
      "ui-nav-menu": React.DetailedHTMLProps<
        React.HTMLAttributes<HTMLElement>,
        HTMLElement
      >;
    }
  }
}

// ---------------------------------------------------------------------------
// Root redirect — preserves Shopify's host/shop/embedded query params so that
// App Bridge stays healthy after React Router's initial navigation.
// ---------------------------------------------------------------------------

/**
 * Redirects / → /products while keeping the query string intact.
 * Without this, BrowserRouter's <Navigate to="/products" replace /> would
 * silently drop `?host=...&shop=...&embedded=1`, causing App Bridge to lose
 * the host param and subsequently return expired/stale session tokens.
 */
function RootRedirect() {
  const location = useLocation();
  // Carry the full query string (and hash) over to /products so the host param
  // remains in window.location.search after the redirect.
  return (
    <Navigate
      to={{ pathname: "/products", search: location.search, hash: location.hash }}
      replace
    />
  );
}
// App Bridge is initialized in main.tsx (before React renders) — see main.tsx.
// We deliberately do NOT call initAppBridge() here so importing App.tsx in tests
// has no side effects.
import { EntityLibraryScreen } from "./screens/EntityLibraryScreen";
import { WarningTemplateLibraryScreen } from "./screens/WarningTemplateLibraryScreen";
// F5's RulesScreen is now available — use a static import.
import { RulesScreen } from "./screens/RulesScreen";
// F6's ProductsScreen — bulk product editor + compliance status.
import { ProductsScreen } from "./screens/ProductsScreen";

// ---------------------------------------------------------------------------
// Embedded-app sidebar nav via App Bridge web component
//
// <ui-nav-menu> is provided by the CDN App Bridge script (index.html).
// App Bridge lifts these <a> links into the Shopify Admin left sidebar
// (outside the iframe).  rel="home" marks the primary / home route.
//
// Clicking a link triggers a same-page navigation to the href — React Router's
// BrowserRouter intercepts it as a client-side navigation (no full reload).
// ---------------------------------------------------------------------------

function AppNavMenu() {
  return (
    <ui-nav-menu>
      <a href="/products" rel="home">
        Products
      </a>
      <a href="/entities">Entities</a>
      <a href="/warning-templates">Warning Templates</a>
      <a href="/rules">Rules</a>
    </ui-nav-menu>
  );
}

// ---------------------------------------------------------------------------
// Root app
// ---------------------------------------------------------------------------

export function App() {
  return (
    <AppProvider i18n={enTranslations}>
      <BrowserRouter>
        {/*
          AppNavMenu renders outside <Routes> so it is always present in the
          DOM regardless of which route is active.  App Bridge reads it once
          during bootstrap; subsequent re-renders do not affect the sidebar.
        */}
        <AppNavMenu />
        <Routes>
          <Route path="/" element={<RootRedirect />} />
          <Route path="/products" element={<ProductsScreen />} />
          <Route path="/entities" element={<EntityLibraryScreen />} />
          <Route
            path="/warning-templates"
            element={<WarningTemplateLibraryScreen />}
          />
          <Route path="/rules" element={<RulesScreen />} />
        </Routes>
      </BrowserRouter>
    </AppProvider>
  );
}
