import standard from '../../packages/stow-s3/eslint.config.mjs';
import globals from '../../packages/stow-s3/node_modules/globals/index.js';
export default [...standard,
  { files: ['src/**/*.ts'], languageOptions: { globals: globals.browser } },
  { files: ['*.mjs', 'test/*.mjs'], languageOptions: { globals: globals.node }, rules: {
    'no-console': 'off', '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
  } },
];
