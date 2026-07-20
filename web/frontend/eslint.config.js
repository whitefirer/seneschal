import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'

// Gate philosophy: recommended-level rule sets, tuned to guard real bugs
// without forcing a codebase-wide style rewrite ahead of the component split
// (Execution.tsx / WorkflowGraphEditor.tsx / WorkflowGraph.tsx).
//
// Tuning decisions (vs. raw recommended):
// - @typescript-eslint/no-explicit-any → 'warn'. The three giant components
//   and the dag utils use `any` pervasively for step/node payloads. As an
//   error it produces hundreds of violations with zero behavior change when
//   fixed; keep it visible as a warning and tighten after the split.
// - react-refresh/only-export-components → 'warn' (Vite template default).
//   Mixed exports only affect HMR granularity, not correctness.
// - react-hooks/exhaustive-deps → kept at the plugin-recommended 'warn'.
//   20 sites, all in the three giant components; fixing them means editing
//   effect/memo dependency arrays, which changes re-run cadence and is
//   unverifiable without component tests. rules-of-hooks stays 'error'.
//   Revisit as each component is split.
// - ban-ts-comment → ts-nocheck/ts-expect-error need a description.
//   WorkflowGraphEditor.tsx 的 @ts-nocheck 已在组件拆分（C.8）中移除，
//   两套 GraphNode 定义已合并到 src/types/graph.ts。该规则保持开启，
//   防止新的无说明类型抑制回潮。
export default tseslint.config(
  { ignores: ['dist', 'node_modules'] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
      '@typescript-eslint/no-explicit-any': 'warn',
      '@typescript-eslint/ban-ts-comment': [
        'error',
        {
          'ts-expect-error': 'allow-with-description',
          'ts-ignore': true,
          'ts-nocheck': 'allow-with-description',
        },
      ],
    },
  },
)
