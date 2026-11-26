import { createRouter, createWebHashHistory } from "vue-router";

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: "/",
      name: "dashboard",
      component: () => import("./pages/DashboardPage.vue"),
      meta: { title: "总览" },
    },
    {
      path: "/vms",
      name: "vms",
      component: () => import("./pages/VirtualMachinesPage.vue"),
      meta: { title: "虚拟机" },
    },
    {
      path: "/create",
      name: "create",
      component: () => import("./pages/CreateVMPage.vue"),
      meta: { title: "新建虚拟机" },
    },
    {
      path: "/clone",
      name: "clone",
      component: () => import("./pages/CloneVMPage.vue"),
      meta: { title: "克隆虚拟机" },
    },
    {
      path: "/tasks",
      name: "tasks",
      component: () => import("./pages/TasksPage.vue"),
      meta: { title: "任务队列" },
    },
    {
      path: "/network",
      name: "network",
      component: () => import("./pages/NetworksPage.vue"),
      meta: { title: "虚拟交换机" },
    },
    {
      path: "/setup",
      name: "setup",
      component: () => import("./pages/SetupPage.vue"),
      meta: { title: "环境检查" },
    },
    {
      path: "/host",
      name: "host",
      component: () => import("./pages/HostToolsPage.vue"),
      meta: { title: "主机与工具" },
    },
    {
      path: "/api-docs",
      name: "api-docs",
      component: () => import("./pages/ApiDocsPage.vue"),
      meta: { title: "API 文档" },
    },
    {
      path: "/settings",
      name: "settings",
      component: () => import("./pages/SettingsPage.vue"),
      meta: { title: "设置" },
    },
    { path: "/:pathMatch(.*)*", redirect: "/" },
  ],
});

export default router;
