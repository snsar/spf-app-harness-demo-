/* ESLint config for the GPSR admin (flat config not used; classic .eslintrc). */
module.exports = {
  root: true,
  env: { browser: true, es2020: true },
  extends: [
    "eslint:recommended",
    "plugin:@typescript-eslint/recommended",
  ],
  ignorePatterns: ["dist", "node_modules", ".eslintrc.cjs", "vite.config.ts"],
  parser: "@typescript-eslint/parser",
  plugins: ["react-refresh", "react-hooks"],
  rules: {
    ...require("eslint-plugin-react-hooks").configs.recommended.rules,
    "react-refresh/only-export-components": [
      "warn",
      { allowConstantExport: true },
    ],
  },
};
