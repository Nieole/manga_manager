# React + TypeScript + Vite

This template provides a minimal setup to get React working in Vite with HMR and some ESLint rules.

Currently, two official plugins are available:

- [@vitejs/plugin-react](https://github.com/vitejs/vite-plugin-react/blob/main/packages/plugin-react) uses [Babel](https://babeljs.io/) (or [oxc](https://oxc.rs) when used in [rolldown-vite](https://vite.dev/guide/rolldown)) for Fast Refresh
- [@vitejs/plugin-react-swc](https://github.com/vitejs/vite-plugin-react/blob/main/packages/plugin-react-swc) uses [SWC](https://swc.rs/) for Fast Refresh

## React Compiler

The React Compiler is not enabled on this template because of its impact on dev & build performances. To add it, see [this documentation](https://react.dev/learn/react-compiler/installation).

## Expanding the ESLint configuration

If you are developing a production application, we recommend updating the configuration to enable type-aware lint rules:

```js
export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      // Other configs...

      // Remove tseslint.configs.recommended and replace with this
      tseslint.configs.recommendedTypeChecked,
      // Alternatively, use this for stricter rules
      tseslint.configs.strictTypeChecked,
      // Optionally, add this for stylistic rules
      tseslint.configs.stylisticTypeChecked,

      // Other configs...
    ],
    languageOptions: {
      parserOptions: {
        project: ['./tsconfig.node.json', './tsconfig.app.json'],
        tsconfigRootDir: import.meta.dirname,
      },
      // other options...
    },
  },
])
```

You can also install [eslint-plugin-react-x](https://github.com/Rel1cx/eslint-react/tree/main/packages/plugins/eslint-plugin-react-x) and [eslint-plugin-react-dom](https://github.com/Rel1cx/eslint-react/tree/main/packages/plugins/eslint-plugin-react-dom) for React-specific lint rules:

```js
// eslint.config.js
import reactX from 'eslint-plugin-react-x'
import reactDom from 'eslint-plugin-react-dom'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      // Other configs...
      // Enable lint rules for React
      reactX.configs['recommended-typescript'],
      // Enable lint rules for React DOM
      reactDom.configs.recommended,
    ],
    languageOptions: {
      parserOptions: {
        project: ['./tsconfig.node.json', './tsconfig.app.json'],
        tsconfigRootDir: import.meta.dirname,
      },
      // other options...
    },
  },
])
```

## 依赖安全审计（npm audit）

`npm audit` 目前会报出两条 high，二者都**无法**通过升级消除，且都已确认对本项目不可达。
新增依赖后请复核这份清单是否仍然成立，不要因为「审计有红」就盲目 `npm audit fix --force`。

### 1. react-router — GHSA-qwww-vcr4-c8h2（RSC Mode CSRF Bypass）

- **不适用**：该漏洞只存在于 React Server Components 模式。本项目用的是纯客户端路由
  （`BrowserRouter` + `Routes/Route`），全量使用的 API 仅有 `BrowserRouter / Link /
  Navigate / Outlet / Route / Routes / UNSAFE_NavigationContext / useLocation /
  useNavigate / useOutletContext / useParams / useSearchParams`，
  没有 data router（`createBrowserRouter` / `loader` / `action` / `useFetcher`），
  也没有任何 server action，漏洞代码路径不可达。
- **不要跑 `npm audit fix --force`**：它给出的「修复」是把 react-router-dom **降级**到
  7.11.0，等于为了一条不适用的告警回退 7 个小版本的修复与特性。
- 真正的修复在 react-router 8.3.0+，属于跨大版本升级，需要单独评估。

### 2. brace-expansion — GHSA-mh99-v99m-4gvg（DoS via unbounded expansion）

- **仅构建期**：链路是 `eslint → @eslint/config-array/eslintrc → minimatch →
  brace-expansion`，只存在于 devDependencies，不会进入产物；且展开的 glob 是我们自己
  写在 `eslint.config.js` 里的模式，不接受外部输入。
- **上游阻塞**：唯一打过补丁的版本是 `brace-expansion@5.0.8`，它改变了导出形态，
  当前 `minimatch` 调用 `expand(...)` 会直接抛 `TypeError: expand is not a function`
  （已实测）。要等 eslint/minimatch 侧跟进后才能解。

### 已处理的部分

`axios` 1.16.1→1.18.1、`react-router-dom` 7.16.0→7.18.1，以及 `postcss` / `js-yaml` /
`form-data` 的传递依赖均已升到修复版本；`esbuild` 经 `overrides` 升到 0.28.1
（GHSA-g7r4-m6w7-qqqr，Windows 开发服务器任意文件读，low）。
