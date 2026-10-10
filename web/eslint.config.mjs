import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

export default defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    rules: {
      // The 4 call sites this fires on are all intentional: syncing React state
      // from an external, client-only source after mount (sessionStorage,
      // localStorage) so the server-rendered HTML never depends on it and
      // hydration can't mismatch, or syncing editable state from a changed
      // prop. Rewriting them away from useEffect would either reintroduce a
      // hydration bug or require a data-fetching library this app doesn't use.
      "react-hooks/set-state-in-effect": "warn",
    },
  },
  globalIgnores([".next/**", "out/**", "build/**", "next-env.d.ts"]),
]);
