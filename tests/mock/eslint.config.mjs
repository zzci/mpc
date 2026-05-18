import antfu from '@antfu/eslint-config'

export default antfu({
  type: 'lib',
  typescript: true,
  formatters: false,
  // Test-only double; it is a Bun program, not a browser/node lib.
  ignores: ['testdata/**', 'node_modules/**'],
})
