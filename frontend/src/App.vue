<script setup lang="ts">
import { darkTheme, dateZhCN, zhCN, type GlobalThemeOverrides } from "naive-ui";
import AppShell from "./components/AppShell.vue";

const themeOverrides: GlobalThemeOverrides = {
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
    fontFamily: '"Segoe UI", "Microsoft YaHei UI", "PingFang SC", system-ui, sans-serif',
    fontSize: "13px",
  },
  Card: {
    borderRadius: "10px",
  },
  Menu: {
    itemColorActive: "#1b2740",
    itemColorActiveHover: "#1b2740",
    itemTextColorActive: "#8fb6ff",
    itemTextColorActiveHover: "#a5c4ff",
    itemIconColorActive: "#8fb6ff",
    itemIconColorActiveHover: "#a5c4ff",
    itemHeight: "38px",
  },
  Button: {
    borderRadiusMedium: "7px",
    borderRadiusSmall: "6px",
  },
};
</script>

<template>
  <n-config-provider
    :theme="darkTheme"
    :theme-overrides="themeOverrides"
    :locale="zhCN"
    :date-locale="dateZhCN"
  >
    <n-message-provider>
      <n-dialog-provider>
        <n-notification-provider>
          <AppShell />
        </n-notification-provider>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>
