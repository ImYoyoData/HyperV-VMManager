import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import wails from "@wailsio/runtime/plugins/vite";
import Components from "unplugin-vue-components/vite";
import { NaiveUiResolver } from "unplugin-vue-components/resolvers";

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [
    vue(),
    wails("./bindings"),
    Components({
      resolvers: [NaiveUiResolver()],
      dts: "src/components.d.ts",
    }),
  ],
});
