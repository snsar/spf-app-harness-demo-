/**
 * App — root of the GPSR Compliance Engine admin (embedded in Shopify Admin).
 *
 * Shell responsibilities:
 *  - Wrap the app in Polaris AppProvider (CSS + en translations)
 *  - Wrap in Polaris Frame with Navigation for the app nav
 *  - Provide client-side routing via React Router
 *  - Route: /entities → EntityLibraryScreen (F4)
 *  - Route: /warning-templates → WarningTemplateLibraryScreen (F4)
 *  - Route: /rules → placeholder (slot for F5's RulesScreen)
 *  - Default route → redirect to /entities
 *
 * Shared files F5 also uses: App.tsx (AppProvider wraps F5's RulesScreen),
 * src/api/client.ts (shared fetch wrapper), src/api/types.ts (shared types).
 * F5 should NOT modify App.tsx's AppProvider setup or the navigation shell —
 * only add new <Route> entries for rules.
 */

import "@shopify/polaris/build/esm/styles.css";
import { AppProvider, Frame, Navigation } from "@shopify/polaris";
import enTranslations from "@shopify/polaris/locales/en.json";
import {
  BrowserRouter,
  Routes,
  Route,
  Navigate,
  useLocation,
  useNavigate,
} from "react-router-dom";

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
// Navigation shell (Polaris Frame + Navigation)
// ---------------------------------------------------------------------------

const NAV_ITEMS = [
  { label: "Products", path: "/products" },
  { label: "Entities", path: "/entities" },
  { label: "Warning Templates", path: "/warning-templates" },
  { label: "Rules", path: "/rules" },
] as const;

function AppNavigation() {
  const location = useLocation();
  const navigate = useNavigate();

  return (
    <Navigation location={location.pathname}>
      <Navigation.Section
        title="GPSR Compliance"
        items={NAV_ITEMS.map(({ label, path }) => ({
          label,
          selected: location.pathname.startsWith(path),
          // NOTE: intentionally NO `url` prop. With `url` set, Polaris renders an
          // <a href={url}> and a click makes the browser FOLLOW the href — a full
          // page reload inside the Shopify iframe that re-bootstraps App Bridge
          // every time (the menu "flicker"). Using onClick-only keeps navigation
          // purely client-side via React Router (no reload, no flicker).
          onClick: () => {
            if (location.pathname !== path) {
              navigate(path);
            }
          },
        }))}
      />
    </Navigation>
  );
}

// ---------------------------------------------------------------------------
// Root app
// ---------------------------------------------------------------------------

export function App() {
  const logo = {
    width: 124,
    topBarSource: "",
    contextualSaveBarSource: "",
    url: "/",
    label: "GPSR",
  };

  return (
    <AppProvider i18n={enTranslations}>
      <BrowserRouter>
        <Frame logo={logo} navigation={<AppNavigation />}>
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
        </Frame>
      </BrowserRouter>
    </AppProvider>
  );
}
