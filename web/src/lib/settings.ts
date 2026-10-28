import { writable } from "svelte/store";
import { getJSON, putJSON } from "./api";
import type { MasterPasswordState } from "./notes";

interface SettingsResponse {
  name: string;
  name_set: boolean;
}

export interface SettingsState {
  status: "loading" | "ready" | "error";
  name: string;
  nameSet: boolean;
  // 引导页判据「名字已保存 且 主密码已配置」需要两套数据；缺主密码那半套时整体进 error 态。
  masterConfigured: boolean;
  error: string;
}

export const settings = writable<SettingsState>({
  status: "loading",
  name: "",
  nameSet: false,
  masterConfigured: false,
  error: "",
});

export async function loadSettings(): Promise<void> {
  try {
    const [data, master] = await Promise.all([
      getJSON<SettingsResponse>("/api/settings"),
      getJSON<MasterPasswordState>("/api/master-password"),
    ]);
    settings.update((state) => ({
      ...state,
      status: "ready",
      name: data.name,
      nameSet: data.name_set,
      masterConfigured: master.configured,
      error: "",
    }));
  } catch (error) {
    settings.update((state) => ({
      ...state,
      status: "error",
      error: error instanceof Error ? error.message : "无法读取设置",
    }));
  }
}

export async function saveName(name: string): Promise<void> {
  const data = await putJSON<SettingsResponse>("/api/settings/name", { name });
  settings.update((state) => ({
    ...state,
    status: "ready",
    name: data.name,
    nameSet: data.name_set,
  }));
}
