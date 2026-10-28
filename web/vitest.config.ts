import { svelte } from "@sveltejs/vite-plugin-svelte";
import { defineConfig } from "vitest/config";

export default defineConfig({
  // 组件测试要导入 .svelte 文件：装上 Svelte 插件（自动读取 svelte.config.js），
  // 并把解析条件切到 browser，否则 svelte 会解析到服务端构建（mount 不可用）。
  plugins: [svelte()],
  resolve: { conditions: ["browser"] },
  test: {
    include: ["src/**/*.test.ts"],
  },
});
