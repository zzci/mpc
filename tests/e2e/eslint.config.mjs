// @antfu/eslint-config — no Prettier, per pma-bun baseline.
import antfu from '@antfu/eslint-config'

export default antfu({
  type: 'lib',
  typescript: { tsconfigPath: './tsconfig.json' },
  stylistic: true,
  // docker/ holds E2E-002 infra (Dockerfiles, compose, yaml, shell) — not the
  // TS source/assertion surface this lint gate governs; the TS adapter+tests
  // outside docker/ are still linted.
  ignores: ['node_modules', 'dist', '.bin', 'docker/**'],
})
