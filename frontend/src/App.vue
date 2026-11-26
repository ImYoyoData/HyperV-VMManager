<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { darkTheme, dateZhCN, zhCN, type GlobalThemeOverrides } from "naive-ui";
import AppShell from "./components/AppShell.vue";
import { useThemeStore } from "./stores/theme";

const route = useRoute();
const theme = useThemeStore();
theme.init();

const isPublicRoute = () => Boolean(route.meta.public);
const naiveTheme = computed(() => (theme.isDark ? darkTheme : null));

const fontFamily = '"Segoe UI", "Microsoft YaHei UI", "PingFang SC", system-ui, sans-serif';

const darkOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: "#4f8cff",
    primaryColorHover: "#6ba1ff",
    primaryColorPressed: "#3c76e6",
    primaryColorSuppl: "#4f8cff",
    infoColor: "#5aa9ff",
    successColor: "#49c78b",
    warningColor: "#e0a95e",
    errorColor: "#ef6464",
    textColorBase: "#e7eaf1",
    bodyColor: "#0e1014",
    cardColor: "#14171f",
    modalColor: "#14171f",
    popoverColor: "#181c26",
    tableColor: "#14171f",
    tableHeaderColor: "#181c26",
    inputColor: "#10131a",
    borderColor: "#232734",
    dividerColor: "#20242f",
    borderRadius: "8px",
    fontFamily,
    fontSize: "13px",
  },
  Card: { borderRadius: "10px" },
  Menu: {
    itemColorActive: "#1b2740",
    itemColorActiveHover: "#1b2740",
    itemTextColorActive: "#8fb6ff",
    itemTextColorActiveHover: "#a5c4ff",
    itemIconColorActive: "#8fb6ff",
    itemIconColorActiveHover: "#a5c4ff",
    itemHeight: "38px",
  },
  Button: { borderRadiusMedium: "7px", borderRadiusSmall: "6px" },
};

const lightOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: "#2f6fed",
    primaryColorHover: "#4f8cff",
    primaryColorPressed: "#2459c7",
    primaryColorSuppl: "#2f6fed",
    infoColor: "#2f7ad6",
    successColor: "#1f9d66",
    warningColor: "#b97a1a",
    errorColor: "#d64545",
    textColorBase: "#1f2430",
    bodyColor: "#f5f6f8",
    cardColor: "#ffffff",
    modalColor: "#ffffff",
    popoverColor: "#ffffff",
    tableColor: "#ffffff",
    tableHeaderColor: "#f7f8fa",
    inputColor: "#ffffff",
    borderColor: "#dfe3ea",
    dividerColor: "#e8ebf0",
    textColor1: "#111827",
    textColor2: "#374151",
    textColor3: "#6b7280",
    borderRadius: "8px",
    fontFamily,
    fontSize: "13px",
  },
  Card: { borderRadius: "10px" },
  Menu: {
    itemColorActive: "#eaf1ff",
    itemColorActiveHover: "#eaf1ff",
    itemTextColorActive: "#2f6fed",
    itemTextColorActiveHover: "#2f6fed",
    itemIconColorActive: "#2f6fed",
    itemIconColorActiveHover: "#2f6fed",
    itemHeight: "38px",
  },
  Button: { borderRadiusMedium: "7px", borderRadiusSmall: "6px" },
};

const themeOverrides = computed(() => (theme.isDark ? darkOverrides : lightOverrides));
</script>

<template>
  <n-config-provider
    :theme="naiveTheme"
    :theme-overrides="themeOverrides"
    :locale="zhCN"
    :date-locale="dateZhCN"
  >
    <n-message-provider>
      <n-dialog-provider>
        <n-notification-provider>
          <router-view v-if="isPublicRoute()" />
          <AppShell v-else />
        </n-notification-provider>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>
