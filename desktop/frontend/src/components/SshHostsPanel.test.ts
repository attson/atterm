import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SshHostsPanel from "./SshHostsPanel.vue";
import { resetI18nForTest } from "../i18n";

const listSSHHosts = vi.fn();
const addSSHHost = vi.fn();
const updateSSHHost = vi.fn();
const deleteSSHHost = vi.fn();
const newSshSessionByID = vi.fn();
const listSSHKeys = vi.fn();
const addSSHKey = vi.fn();
const updateSSHKey = vi.fn();
const deleteSSHKey = vi.fn();
const previewSSHConfigImport = vi.fn();
const importSSHHosts = vi.fn();
const startForward = vi.fn();
const stopForward = vi.fn();
const listActiveForwards = vi.fn();
vi.mock("../lib/api", () => ({
  listSSHHosts: (...a: unknown[]) => listSSHHosts(...a),
  addSSHHost: (...a: unknown[]) => addSSHHost(...a),
  updateSSHHost: (...a: unknown[]) => updateSSHHost(...a),
  deleteSSHHost: (...a: unknown[]) => deleteSSHHost(...a),
  newSshSessionByID: (...a: unknown[]) => newSshSessionByID(...a),
  listSSHKeys: (...a: unknown[]) => listSSHKeys(...a),
  addSSHKey: (...a: unknown[]) => addSSHKey(...a),
  updateSSHKey: (...a: unknown[]) => updateSSHKey(...a),
  deleteSSHKey: (...a: unknown[]) => deleteSSHKey(...a),
  previewSSHConfigImport: (...a: unknown[]) => previewSSHConfigImport(...a),
  importSSHHosts: (...a: unknown[]) => importSSHHosts(...a),
  startForward: (...a: unknown[]) => startForward(...a),
  stopForward: (...a: unknown[]) => stopForward(...a),
  listActiveForwards: (...a: unknown[]) => listActiveForwards(...a),
}));

// The panel renders through the i18n layer, so every assertion below that
// reads UI text reads a *resolved* string, not a key. Pin the locale to en so
// those assertions stay stable: they are written against the English messages
// on purpose (asserting on keys instead would stop catching a key that resolves
// to nothing, which is the failure mode worth pinning). The proxy-marker and
// skip-reason assertions in particular have to keep matching real words.
beforeEach(() => {
  resetI18nForTest();
  listSSHHosts.mockReset().mockResolvedValue([]);
  addSSHHost.mockReset();
  updateSSHHost.mockReset();
  deleteSSHHost.mockReset();
  newSshSessionByID.mockReset();
  listSSHKeys.mockReset().mockResolvedValue([]);
  addSSHKey.mockReset();
  updateSSHKey.mockReset();
  deleteSSHKey.mockReset();
  previewSSHConfigImport.mockReset();
  importSSHHosts.mockReset();
  startForward.mockReset().mockResolvedValue(undefined);
  stopForward.mockReset().mockResolvedValue(undefined);
  listActiveForwards.mockReset().mockResolvedValue([]);
});

describe("SshHostsPanel", () => {
  it("挂载时加载主机列表", async () => {
    listSSHHosts.mockResolvedValueOnce([
      { id: "1", host: "h", user: "u", auth_kind: "password", alias: "box" },
    ]);
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    expect(wrapper.text()).toContain("box");
  });

  it("点击某主机连接触发 newSshSessionByID 并 emit connected", async () => {
    listSSHHosts.mockResolvedValueOnce([
      { id: "1", host: "h", user: "u", auth_kind: "password", alias: "box" },
    ]);
    newSshSessionByID.mockResolvedValueOnce({ session_id: "s1" });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await wrapper.find('[data-test="ssh-connect-1"]').trigger("click");
    await flushPromises();
    expect(newSshSessionByID).toHaveBeenCalledWith("1");
    expect(wrapper.emitted("connected")?.[0]).toEqual(["s1"]);
  });

  it("打开新建抽屉、填表单后调用 addSSHHost 并刷新列表", async () => {
    addSSHHost.mockResolvedValueOnce({ id: "2", host: "h2", user: "u2", auth_kind: "password" });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    // The form now lives in a right-side drawer opened via "New Host".
    await wrapper.find('[data-test="ssh-new-host"]').trigger("click");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="ssh-add-host"]').setValue("h2");
    await wrapper.find('[data-test="ssh-add-user"]').setValue("u2");
    await wrapper.find('[data-test="ssh-add-password"]').setValue("pw");
    await wrapper.find('[data-test="ssh-add-submit"]').trigger("click");
    await flushPromises();
    expect(addSSHHost).toHaveBeenCalledWith(
      expect.objectContaining({ host: "h2", user: "u2", auth_kind: "password" }),
      expect.objectContaining({ password: "pw" }),
    );
  });

  it("删除主机调用 deleteSSHHost", async () => {
    listSSHHosts.mockResolvedValueOnce([
      { id: "9", host: "h", user: "u", auth_kind: "password" },
    ]);
    deleteSSHHost.mockResolvedValueOnce(undefined);
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await wrapper.find('[data-test="ssh-delete-9"]').trigger("click");
    await flushPromises();
    expect(deleteSSHHost).toHaveBeenCalledWith("9");
  });

  it("切到 Keys tab 显示密钥并可添加", async () => {
    listSSHKeys.mockResolvedValue([{ id: "k1", name: "aws", key_type: "RSA" }]);
    addSSHKey.mockResolvedValueOnce({ id: "k2", name: "gcp", key_type: "RSA" });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await wrapper.find('[data-test="ssh-tab-keys"]').trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("aws");
    await wrapper.find('[data-test="ssh-key-new"]').trigger("click");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="ssh-key-name"]').setValue("gcp");
    await wrapper.find('[data-test="ssh-key-pem"]').setValue("-----BEGIN-----");
    await wrapper.find('[data-test="ssh-key-submit"]').trigger("click");
    await flushPromises();
    expect(addSSHKey).toHaveBeenCalledWith("gcp", "-----BEGIN-----", "");
  });

  it("主机表单切到 Key 认证时列出密钥库", async () => {
    listSSHKeys.mockResolvedValue([{ id: "k1", name: "aws", key_type: "RSA" }]);
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await wrapper.find('[data-test="ssh-new-host"]').trigger("click");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="ssh-auth-key"]').trigger("click");
    await wrapper.vm.$nextTick();
    // The key picker is a custom SelectDropdown, not a native <select>.
    const picker = wrapper.find('[data-test="ssh-add-keyid"]');
    expect(picker.exists()).toBe(true);
    // Open the dropdown to reveal the options.
    await picker.find('[data-testid="select-trigger"]').trigger("click");
    await wrapper.vm.$nextTick();
    const opts = wrapper.findAll('[data-testid="select-option"]');
    expect(opts.some((o) => o.text().includes("aws"))).toBe(true);
  });

  it("密钥库为空时主机表单的快捷按钮跳去新增 Key", async () => {
    listSSHKeys.mockResolvedValue([]); // empty vault
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await wrapper.find('[data-test="ssh-new-host"]').trigger("click");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="ssh-auth-key"]').trigger("click");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="ssh-host-add-key"]').trigger("click");
    await wrapper.vm.$nextTick();
    // Jumped to Keys tab with the New Key drawer open.
    expect(wrapper.find('[data-test="ssh-tab-keys"]').classes()).toContain("on");
    expect(wrapper.find('[data-test="ssh-key-name"]').exists()).toBe(true);
  });

  it("Tags 字段把已有 tag 作为建议,点击加进来", async () => {
    listSSHHosts.mockResolvedValue([
      { id: "1", host: "h", user: "u", auth_kind: "password", tags: ["prod"] },
      { id: "2", host: "h2", user: "u2", auth_kind: "password", tags: ["staging"] },
    ]);
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await wrapper.find('[data-test="ssh-new-host"]').trigger("click");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="ssh-add-tag-input"]').trigger("focus");
    await wrapper.vm.$nextTick();
    const menu = wrapper.find('[data-test="ssh-tag-menu"]');
    expect(menu.exists()).toBe(true);
    expect(menu.text()).toContain("prod");
    expect(menu.text()).toContain("staging");
    await wrapper.find('[data-test="ssh-tag-opt-staging"]').trigger("mousedown");
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[data-test="ssh-add-tag-chip-staging"]').exists()).toBe(true);
  });
});

describe("SshHostsPanel 主机多 tag", () => {
  const WEB = {
    id: "1", host: "10.0.0.1", user: "root", port: "22",
    auth_kind: "password" as const, alias: "web-1", tags: ["prod", "web"],
  };
  const DB = {
    id: "2", host: "10.0.0.2", user: "root", port: "22",
    auth_kind: "password" as const, alias: "db-1", tags: ["prod", "db"],
  };
  const LOOSE = {
    id: "3", host: "10.0.0.3", user: "root", port: "22",
    auth_kind: "password" as const, alias: "loose",
  };

  async function mountWith(hosts: unknown[]) {
    listSSHHosts.mockResolvedValue(hosts);
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    return wrapper;
  }

  function visibleAliases(wrapper: ReturnType<typeof mount>): string[] {
    return wrapper.findAll('[data-test^="ssh-host-card-"]').map((c) => c.find(".card-label").text());
  }

  it("卡片上显示自己的 tag", async () => {
    const wrapper = await mountWith([WEB]);
    expect(wrapper.find('[data-test="ssh-host-tag-1-prod"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="ssh-host-tag-1-web"]').exists()).toBe(true);
  });

  it("过滤条列出所有在用的 tag,去重且排序", async () => {
    const wrapper = await mountWith([WEB, DB]);
    const bar = wrapper.find('[data-test="ssh-tag-filter-bar"]');
    expect(bar.exists()).toBe(true);
    const labels = bar.findAll('[data-test^="ssh-tag-filter-"]').map((b) => b.text());
    expect(labels).toEqual(["db", "prod", "web"]);
  });

  it("没有任何 tag 时不渲染过滤条", async () => {
    const wrapper = await mountWith([LOOSE]);
    expect(wrapper.find('[data-test="ssh-tag-filter-bar"]').exists()).toBe(false);
  });

  it("点一个 tag 只留下带该 tag 的主机", async () => {
    const wrapper = await mountWith([WEB, DB, LOOSE]);
    await wrapper.find('[data-test="ssh-tag-filter-web"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(visibleAliases(wrapper)).toEqual(["web-1"]);
  });

  it("选中多个 tag 时按 AND 收窄", async () => {
    const wrapper = await mountWith([WEB, DB, LOOSE]);
    await wrapper.find('[data-test="ssh-tag-filter-prod"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(visibleAliases(wrapper)).toEqual(["web-1", "db-1"]);
    await wrapper.find('[data-test="ssh-tag-filter-db"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(visibleAliases(wrapper)).toEqual(["db-1"]);
  });

  it("再点一次已选中的 tag 取消它", async () => {
    const wrapper = await mountWith([WEB, DB, LOOSE]);
    await wrapper.find('[data-test="ssh-tag-filter-web"]').trigger("click");
    await wrapper.find('[data-test="ssh-tag-filter-web"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(visibleAliases(wrapper)).toEqual(["web-1", "db-1", "loose"]);
  });

  it("选中的 tag 带 on 标记", async () => {
    const wrapper = await mountWith([WEB, DB]);
    await wrapper.find('[data-test="ssh-tag-filter-web"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[data-test="ssh-tag-filter-web"]').classes()).toContain("on");
    expect(wrapper.find('[data-test="ssh-tag-filter-prod"]').classes()).not.toContain("on");
  });

  it("Clear 清空全部 tag 选择", async () => {
    const wrapper = await mountWith([WEB, DB, LOOSE]);
    await wrapper.find('[data-test="ssh-tag-filter-web"]').trigger("click");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="ssh-tag-filter-clear"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(visibleAliases(wrapper)).toEqual(["web-1", "db-1", "loose"]);
  });

  it("tag 过滤和搜索框叠加生效", async () => {
    const wrapper = await mountWith([WEB, DB, LOOSE]);
    await wrapper.find('[data-test="ssh-tag-filter-prod"]').trigger("click");
    await wrapper.find('[data-test="ssh-search"]').setValue("db-1");
    await wrapper.vm.$nextTick();
    expect(visibleAliases(wrapper)).toEqual(["db-1"]);
  });

  it("搜索框也能命中 tag 文本", async () => {
    const wrapper = await mountWith([WEB, DB, LOOSE]);
    await wrapper.find('[data-test="ssh-search"]').setValue("db");
    await wrapper.vm.$nextTick();
    expect(visibleAliases(wrapper)).toEqual(["db-1"]);
  });

  it("表单里输入逗号分隔的 tag 会存成数组", async () => {
    addSSHHost.mockResolvedValueOnce({ id: "9" });
    const wrapper = await mountWith([]);
    await wrapper.find('[data-test="ssh-new-host"]').trigger("click");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="ssh-add-host"]').setValue("h9");
    await wrapper.find('[data-test="ssh-add-user"]').setValue("u9");
    await wrapper.find('[data-test="ssh-add-tag-input"]').setValue("prod, db");
    await wrapper.find('[data-test="ssh-add-tag-input"]').trigger("keydown", { key: "Enter" });
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="ssh-add-submit"]').trigger("click");
    await flushPromises();
    expect(addSSHHost).toHaveBeenCalledWith(
      expect.objectContaining({ tags: ["prod", "db"] }),
      expect.anything(),
    );
  });

  it("点 chip 上的 × 移除该 tag", async () => {
    const wrapper = await mountWith([WEB]);
    await wrapper.find('[data-test="ssh-host-card-1"]').trigger("contextmenu");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="session-row-menu-item-edit"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[data-test="ssh-add-tag-chip-web"]').exists()).toBe(true);
    await wrapper.find('[data-test="ssh-add-tag-remove-web"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[data-test="ssh-add-tag-chip-web"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="ssh-add-tag-chip-prod"]').exists()).toBe(true);
  });

  it("编辑主机时保存回去的是改过的 tag 数组", async () => {
    const wrapper = await mountWith([WEB]);
    await wrapper.find('[data-test="ssh-host-card-1"]').trigger("contextmenu");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="session-row-menu-item-edit"]').trigger("click");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="ssh-add-tag-remove-web"]').trigger("click");
    await wrapper.find('[data-test="ssh-add-submit"]').trigger("click");
    await flushPromises();
    expect(updateSSHHost).toHaveBeenCalledWith(
      expect.objectContaining({ id: "1", tags: ["prod"] }),
      null,
    );
  });

  it("重复输入同一个 tag 不会产生两个 chip", async () => {
    const wrapper = await mountWith([]);
    await wrapper.find('[data-test="ssh-new-host"]').trigger("click");
    await wrapper.vm.$nextTick();
    const input = wrapper.find('[data-test="ssh-add-tag-input"]');
    await input.setValue("prod");
    await input.trigger("keydown", { key: "Enter" });
    await input.setValue("PROD");
    await input.trigger("keydown", { key: "Enter" });
    await wrapper.vm.$nextTick();
    expect(wrapper.findAll('[data-test^="ssh-add-tag-chip-"]')).toHaveLength(1);
  });

  it("过滤后某个 tag 被清空时列表显示空态", async () => {
    const wrapper = await mountWith([WEB, DB]);
    await wrapper.find('[data-test="ssh-tag-filter-web"]').trigger("click");
    await wrapper.find('[data-test="ssh-tag-filter-db"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(visibleAliases(wrapper)).toEqual([]);
    expect(wrapper.find('[data-test="ssh-hosts-no-match"]').exists()).toBe(true);
  });
});

describe("SshHostsPanel 主机右键菜单", () => {
  const HOST = {
    id: "1",
    host: "10.0.0.1",
    user: "root",
    port: "2222",
    auth_kind: "key" as const,
    key_id: "k1",
    alias: "box",
    tags: ["prod"],
  };

  async function mountWithHosts(hosts: unknown[] = [HOST]) {
    listSSHHosts.mockResolvedValue(hosts);
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    return wrapper;
  }

  async function openMenu(wrapper: ReturnType<typeof mount>, id = "1") {
    await wrapper.find(`[data-test="ssh-host-card-${id}"]`).trigger("contextmenu");
    await wrapper.vm.$nextTick();
    return wrapper;
  }

  it("右键主机卡片弹出菜单", async () => {
    const wrapper = await mountWithHosts();
    expect(wrapper.find('[data-test="session-row-menu"]').exists()).toBe(false);
    await openMenu(wrapper);
    expect(wrapper.find('[data-test="session-row-menu"]').exists()).toBe(true);
  });

  it("菜单包含 Connect / Edit / Duplicate / Copy SSH command / Remove", async () => {
    const wrapper = await mountWithHosts();
    await openMenu(wrapper);
    for (const key of ["connect", "edit", "duplicate", "copy-ssh", "remove"]) {
      expect(wrapper.find(`[data-test="session-row-menu-item-${key}"]`).exists()).toBe(true);
    }
  });

  it("Remove 上方有分隔线", async () => {
    const wrapper = await mountWithHosts();
    await openMenu(wrapper);
    expect(wrapper.find('[data-test="session-row-menu-separator-remove"]').exists()).toBe(true);
  });

  it("Connect 走和双击一样的连接路径", async () => {
    newSshSessionByID.mockResolvedValueOnce({ session_id: "s1" });
    const wrapper = await mountWithHosts();
    await openMenu(wrapper);
    await wrapper.find('[data-test="session-row-menu-item-connect"]').trigger("click");
    await flushPromises();
    expect(newSshSessionByID).toHaveBeenCalledWith("1");
    expect(wrapper.emitted("connected")?.[0]).toEqual(["s1"]);
  });

  it("Edit Host Details 打开该主机的编辑抽屉", async () => {
    const wrapper = await mountWithHosts();
    await openMenu(wrapper);
    await wrapper.find('[data-test="session-row-menu-item-edit"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect((wrapper.find('[data-test="ssh-add-host"]').element as HTMLInputElement).value).toBe("10.0.0.1");
    expect((wrapper.find('[data-test="ssh-add-alias"]').element as HTMLInputElement).value).toBe("box");
  });

  it("Remove 删除该主机", async () => {
    const wrapper = await mountWithHosts();
    await openMenu(wrapper);
    await wrapper.find('[data-test="session-row-menu-item-remove"]').trigger("click");
    await flushPromises();
    expect(deleteSSHHost).toHaveBeenCalledWith("1");
  });

  it("Duplicate 复制主机的全部字段,label 加 copy 后缀", async () => {
    addSSHHost.mockResolvedValueOnce({ ...HOST, id: "2" });
    const wrapper = await mountWithHosts();
    await openMenu(wrapper);
    await wrapper.find('[data-test="session-row-menu-item-duplicate"]').trigger("click");
    await flushPromises();
    expect(addSSHHost).toHaveBeenCalledWith(
      expect.objectContaining({
        host: "10.0.0.1",
        user: "root",
        port: "2222",
        tags: ["prod"],
        auth_kind: "key",
        key_id: "k1",
        alias: "box copy",
      }),
      expect.anything(),
    );
  });

  it("Duplicate 一个 key 认证的主机后不打开抽屉", async () => {
    addSSHHost.mockResolvedValueOnce({ ...HOST, id: "2" });
    const wrapper = await mountWithHosts();
    await openMenu(wrapper);
    await wrapper.find('[data-test="session-row-menu-item-duplicate"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-test="ssh-add-host"]').exists()).toBe(false);
  });

  it("Duplicate 一个密码认证的主机后打开抽屉,因为副本还没有密码", async () => {
    const pwHost = { ...HOST, auth_kind: "password" as const, key_id: undefined };
    addSSHHost.mockResolvedValueOnce({ ...pwHost, id: "2", alias: "box copy" });
    const wrapper = await mountWithHosts([pwHost]);
    await openMenu(wrapper);
    await wrapper.find('[data-test="session-row-menu-item-duplicate"]').trigger("click");
    await flushPromises();
    expect((wrapper.find('[data-test="ssh-add-alias"]').element as HTMLInputElement).value).toBe("box copy");
  });

  it("Copy SSH command 把 ssh 命令写进剪贴板", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    const original = navigator.clipboard;
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    try {
      const wrapper = await mountWithHosts();
      await openMenu(wrapper);
      await wrapper.find('[data-test="session-row-menu-item-copy-ssh"]').trigger("click");
      await flushPromises();
      expect(writeText).toHaveBeenCalledWith("ssh -p 2222 root@10.0.0.1");
    } finally {
      Object.defineProperty(navigator, "clipboard", { configurable: true, value: original });
    }
  });

  it("右键另一台主机时菜单跟着换目标", async () => {
    const other = { ...HOST, id: "9", host: "10.0.0.9", alias: "other" };
    const wrapper = await mountWithHosts([HOST, other]);
    await openMenu(wrapper, "1");
    await openMenu(wrapper, "9");
    await wrapper.find('[data-test="session-row-menu-item-remove"]').trigger("click");
    await flushPromises();
    expect(deleteSSHHost).toHaveBeenCalledWith("9");
  });

  it("Keys 卡片不挂右键菜单", async () => {
    listSSHKeys.mockResolvedValue([{ id: "k1", name: "aws", key_type: "RSA" }]);
    const wrapper = await mountWithHosts([]);
    await wrapper.find('[data-test="ssh-tab-keys"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[data-test="ssh-host-card-k1"]').exists()).toBe(false);
  });
});

// Throwaway ssh-keygen output; guards nothing.
const PLAIN_ED25519 = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACDnYEHA0nVAFFUzLL6vGE3bxGNxcZ7ls9OzUxFNEKDF6AAAAIhB8hNwQfIT
cAAAAAtzc2gtZWQyNTUxOQAAACDnYEHA0nVAFFUzLL6vGE3bxGNxcZ7ls9OzUxFNEKDF6A
AAAEDxcrxroN3ibzjL7HnAh/KLSPDz/BFih9Oma4YsoodOa+dgQcDSdUAUVTMsvq8YTdvE
Y3FxnuWz07NTEU0QoMXoAAAAAAECAwQF
-----END OPENSSH PRIVATE KEY-----
`;

const ENCRYPTED_ED25519 = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAACmFlczI1Ni1jdHIAAAAGYmNyeXB0AAAAGAAAABCLxP3mcQ
O4lGG2UdK/lXV6AAAAGAAAAAEAAAAzAAAAC3NzaC1lZDI1NTE5AAAAIKSIw7r2JXjv0zDJ
178W6/YoEpaOzKFQ0C6yyisgjOwFAAAAkAsgwIJl5VIO2Q2c8R7fLb8eJR/CxKpgm9U+j/
rCzf3AtWIU5j1bdGPzc0Ea1ZGCGAkNiEgBRn1pEwSZ09xp7pp8SwiWXtPVnVtl88Qbs61a
4ugciL4umcIcTkeHok/A21OGaN1uoBBAaCvG8sQLKvMAlWW0auZfcRmctEVvIuKiy9WO2C
3LY2RDo5EMXHuv/Q==
-----END OPENSSH PRIVATE KEY-----
`;

// Open the Keys tab's New Key drawer on a freshly mounted panel.
async function mountWithKeyDrawer() {
  const wrapper = mount(SshHostsPanel);
  await flushPromises();
  await wrapper.find('[data-test="ssh-tab-keys"]').trigger("click");
  await wrapper.find('[data-test="ssh-key-new"]').trigger("click");
  await wrapper.vm.$nextTick();
  return wrapper;
}

// The import path reads the file through FileReader, which settles on a
// macrotask — flushPromises alone (microtasks only) would sample too early.
async function settleFileRead(wrapper: ReturnType<typeof mount>) {
  await new Promise((r) => setTimeout(r, 0));
  await flushPromises();
  await wrapper.vm.$nextTick();
}

// jsdom's input.files is read-only; override it, then fire change.
async function pickFile(wrapper: ReturnType<typeof mount>, file: File) {
  const input = wrapper.find('[data-test="ssh-key-file-input"]');
  Object.defineProperty(input.element, "files", { value: [file], configurable: true });
  await input.trigger("change");
  await settleFileRead(wrapper);
}

async function dropFile(wrapper: ReturnType<typeof mount>, file: File) {
  await wrapper.find('[data-test="ssh-key-dropzone"]').trigger("drop", {
    dataTransfer: { files: [file] },
  });
  await settleFileRead(wrapper);
}

function pemValue(wrapper: ReturnType<typeof mount>): string {
  return (wrapper.find('[data-test="ssh-key-pem"]').element as HTMLTextAreaElement).value;
}
function nameValue(wrapper: ReturnType<typeof mount>): string {
  return (wrapper.find('[data-test="ssh-key-name"]').element as HTMLInputElement).value;
}

describe("SshHostsPanel 私钥文件导入", () => {
  it("New Key 抽屉里有导入按钮和拖拽区", async () => {
    const wrapper = await mountWithKeyDrawer();
    expect(wrapper.find('[data-test="ssh-key-import-btn"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="ssh-key-dropzone"]').exists()).toBe(true);
  });

  it("点击导入按钮会打开隐藏的文件选择框", async () => {
    const wrapper = await mountWithKeyDrawer();
    const input = wrapper.find('[data-test="ssh-key-file-input"]').element as HTMLInputElement;
    const click = vi.spyOn(input, "click");
    await wrapper.find('[data-test="ssh-key-import-btn"]').trigger("click");
    expect(click).toHaveBeenCalled();
  });

  it("选中私钥文件后填入 PEM 并用文件名填 Name", async () => {
    const wrapper = await mountWithKeyDrawer();
    await pickFile(wrapper, new File([PLAIN_ED25519], "id_ed25519"));
    expect(pemValue(wrapper)).toBe(PLAIN_ED25519);
    expect(nameValue(wrapper)).toBe("id_ed25519");
  });

  it("Name 已手填时导入不覆盖它", async () => {
    const wrapper = await mountWithKeyDrawer();
    await wrapper.find('[data-test="ssh-key-name"]').setValue("my-label");
    await pickFile(wrapper, new File([PLAIN_ED25519], "aws.pem"));
    expect(nameValue(wrapper)).toBe("my-label");
    expect(pemValue(wrapper)).toBe(PLAIN_ED25519);
  });

  it("拖拽私钥文件到虚线区同样填入 PEM", async () => {
    const wrapper = await mountWithKeyDrawer();
    await dropFile(wrapper, new File([PLAIN_ED25519], "aws.pem"));
    expect(pemValue(wrapper)).toBe(PLAIN_ED25519);
    expect(nameValue(wrapper)).toBe("aws");
  });

  it("dragover 时虚线区进入高亮态,dragleave 后恢复", async () => {
    const wrapper = await mountWithKeyDrawer();
    const zone = wrapper.find('[data-test="ssh-key-dropzone"]');
    expect(zone.classes()).not.toContain("over");
    await zone.trigger("dragover");
    expect(wrapper.find('[data-test="ssh-key-dropzone"]').classes()).toContain("over");
    await zone.trigger("dragleave");
    expect(wrapper.find('[data-test="ssh-key-dropzone"]').classes()).not.toContain("over");
  });

  it("导入加密私钥时提示需要 passphrase", async () => {
    const wrapper = await mountWithKeyDrawer();
    expect(wrapper.find('[data-test="ssh-key-encrypted-hint"]').exists()).toBe(false);
    await pickFile(wrapper, new File([ENCRYPTED_ED25519], "prod.pem"));
    expect(wrapper.find('[data-test="ssh-key-encrypted-hint"]').exists()).toBe(true);
  });

  it("导入未加密私钥时不显示 passphrase 提示", async () => {
    const wrapper = await mountWithKeyDrawer();
    await pickFile(wrapper, new File([PLAIN_ED25519], "id_ed25519"));
    expect(wrapper.find('[data-test="ssh-key-encrypted-hint"]').exists()).toBe(false);
  });

  it("误拖公钥文件时报错且不写入 PEM", async () => {
    const wrapper = await mountWithKeyDrawer();
    await dropFile(wrapper, new File(["ssh-ed25519 AAAAC3Nz user@host\n"], "id_ed25519.pub"));
    expect(pemValue(wrapper)).toBe("");
    expect(wrapper.find('[data-test="ssh-hosts-error"]').text()).toContain("not a private key file");
  });

  it("重新打开 New Key 抽屉会清掉上一次的加密提示", async () => {
    const wrapper = await mountWithKeyDrawer();
    await pickFile(wrapper, new File([ENCRYPTED_ED25519], "prod.pem"));
    expect(wrapper.find('[data-test="ssh-key-encrypted-hint"]').exists()).toBe(true);
    await wrapper.find('[data-test="ssh-key-submit"]').trigger("click");
    await flushPromises();
    await wrapper.find('[data-test="ssh-key-new"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[data-test="ssh-key-encrypted-hint"]').exists()).toBe(false);
  });

  it("面板打开时吞掉落在拖拽区之外的拖放,避免 webview 直接打开该文件", async () => {
    await mountWithKeyDrawer();
    const drop = new Event("drop", { bubbles: true, cancelable: true });
    window.dispatchEvent(drop);
    expect(drop.defaultPrevented).toBe(true);
    const dragover = new Event("dragover", { bubbles: true, cancelable: true });
    window.dispatchEvent(dragover);
    expect(dragover.defaultPrevented).toBe(true);
  });

  it("面板卸载后不再拦截全局拖放", async () => {
    const wrapper = await mountWithKeyDrawer();
    wrapper.unmount();
    const drop = new Event("drop", { bubbles: true, cancelable: true });
    window.dispatchEvent(drop);
    expect(drop.defaultPrevented).toBe(false);
  });

  it("触屏设备上不渲染拖拽区,但保留导入按钮", async () => {
    const original = window.matchMedia;
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      writable: true,
      value: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
    });
    try {
      const wrapper = await mountWithKeyDrawer();
      expect(wrapper.find('[data-test="ssh-key-dropzone"]').exists()).toBe(false);
      expect(wrapper.find('[data-test="ssh-key-import-btn"]').exists()).toBe(true);
    } finally {
      if (original) {
        Object.defineProperty(window, "matchMedia", { configurable: true, writable: true, value: original });
      } else {
        delete (window as unknown as Record<string, unknown>).matchMedia;
      }
    }
  });
});

// Open the ssh_config import drawer on a freshly mounted panel.
async function openConfigImportDrawer(wrapper: ReturnType<typeof mount>) {
  await wrapper.find('[data-test="ssh-config-import-open"]').trigger("click");
  await flushPromises();
  await wrapper.vm.$nextTick();
}

describe("SshHostsPanel ssh_config 导入", () => {
  it("预览成功后展示可导入主机与跳过项(带原因)", async () => {
    previewSSHConfigImport.mockResolvedValueOnce({
      entries: [
        { id: "", alias: "web1", host: "10.0.0.1", user: "root", auth_kind: "password" },
      ],
      skipped: [{ alias: "staging-*", reason: "Match block not supported, skipped" }],
      note: "Only fields atterm uses are imported",
    });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await openConfigImportDrawer(wrapper);
    expect(wrapper.text()).toContain("web1");
    const skipped = wrapper.find('[data-test="ssh-config-import-skipped"]');
    expect(skipped.exists()).toBe(true);
    expect(skipped.text()).toContain("staging-*");
    expect(skipped.text()).toContain("Match block not supported, skipped");
  });

  it("默认不勾选任何一行,直接点导入不会调用 importSSHHosts", async () => {
    previewSSHConfigImport.mockResolvedValueOnce({
      entries: [
        { id: "", alias: "web1", host: "10.0.0.1", user: "root", auth_kind: "password" },
      ],
      skipped: [],
      note: "Only fields atterm uses are imported",
    });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await openConfigImportDrawer(wrapper);
    const checkbox = wrapper.find('[data-test="ssh-config-entry-check-0"]').element as HTMLInputElement;
    expect(checkbox.checked).toBe(false);
    const confirmBtn = wrapper.find('[data-test="ssh-config-import-confirm"]');
    expect((confirmBtn.element as HTMLButtonElement).disabled).toBe(true);
    await confirmBtn.trigger("click");
    await flushPromises();
    expect(importSSHHosts).not.toHaveBeenCalled();
  });

  it("勾选后确认导入,只传入选中的主机并展示导入数量", async () => {
    previewSSHConfigImport.mockResolvedValueOnce({
      entries: [
        { id: "", alias: "web1", host: "10.0.0.1", user: "root", auth_kind: "password" },
        { id: "", alias: "web2", host: "10.0.0.2", user: "root", auth_kind: "password" },
      ],
      skipped: [],
      note: "Only fields atterm uses are imported",
    });
    importSSHHosts.mockResolvedValueOnce(1);
    listSSHHosts.mockResolvedValue([]);
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await openConfigImportDrawer(wrapper);
    await wrapper.find('[data-test="ssh-config-entry-check-0"]').trigger("change");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="ssh-config-import-confirm"]').trigger("click");
    await flushPromises();
    expect(importSSHHosts).toHaveBeenCalledTimes(1);
    const [passed] = importSSHHosts.mock.calls[0] as [{ alias?: string }[]];
    expect(passed).toHaveLength(1);
    expect(passed[0].alias).toBe("web1");
    expect(wrapper.find('[data-test="ssh-config-import-result"]').text()).toContain("1");
  });

  it("proxy_jump/proxy_command 的主机带跳板机提示标记", async () => {
    previewSSHConfigImport.mockResolvedValueOnce({
      entries: [
        {
          id: "", alias: "inner", host: "10.0.0.9", user: "root", auth_kind: "password",
          proxy_jump: "bastion",
        },
      ],
      skipped: [],
      note: "Only fields atterm uses are imported",
    });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await openConfigImportDrawer(wrapper);
    const badge = wrapper.find('[data-test="ssh-config-entry-proxy-0"]');
    expect(badge.exists()).toBe(true);
    expect(badge.text()).toMatch(/jump host|ProxyJump/i);
  });

  it("预览请求失败时展示错误信息,而不是空列表", async () => {
    previewSSHConfigImport.mockRejectedValueOnce(new Error("read ~/.ssh/config: permission denied"));
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await openConfigImportDrawer(wrapper);
    const err = wrapper.find('[data-test="ssh-config-import-error"]');
    expect(err.exists()).toBe(true);
    expect(err.text()).toContain("permission denied");
    expect(wrapper.find('[data-test="ssh-config-import-empty"]').exists()).toBe(false);
    // Drawer stays open and usable, not force-closed on error.
    expect(wrapper.find('[data-test="ssh-config-import-confirm"]').exists()).toBe(true);
  });

  it("entries 和 skipped 都为空时展示空态而不是报错", async () => {
    previewSSHConfigImport.mockResolvedValueOnce({ entries: [], skipped: [], note: "Only fields atterm uses are imported" });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await openConfigImportDrawer(wrapper);
    expect(wrapper.find('[data-test="ssh-config-import-empty"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="ssh-config-import-error"]').exists()).toBe(false);
  });

  it("entries 为空但有 skipped 时展示跳过列表,不是空态", async () => {
    previewSSHConfigImport.mockResolvedValueOnce({
      entries: [],
      skipped: [{ alias: "weird", reason: "Include path unreadable" }],
      note: "Only fields atterm uses are imported",
    });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await openConfigImportDrawer(wrapper);
    expect(wrapper.find('[data-test="ssh-config-import-empty"]').exists()).toBe(false);
    const skipped = wrapper.find('[data-test="ssh-config-import-skipped"]');
    expect(skipped.exists()).toBe(true);
    expect(skipped.text()).toContain("Include path unreadable");
  });

  it("抽屉底部展示后端返回的 note", async () => {
    previewSSHConfigImport.mockResolvedValueOnce({
      entries: [{ id: "", alias: "web1", host: "10.0.0.1", user: "root", auth_kind: "password" }],
      skipped: [],
      note: "Only fields atterm uses are imported; other settings (e.g. ControlMaster) are ignored",
    });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await openConfigImportDrawer(wrapper);
    expect(wrapper.text()).toContain("Only fields atterm uses are imported");
  });

  // Every other mock in this file hard-codes `skipped: []`, which is exactly
  // why a nil Go slice marshalling to JSON null went unnoticed: the drawer
  // reads .skipped.length, so `null` throws a TypeError and takes the whole
  // preview down. The Go side now make()s the slice; this pins the frontend
  // guard so a regression on either side can't reopen it.
  it("skipped 为 null 时不炸,照常渲染条目列表", async () => {
    previewSSHConfigImport.mockResolvedValueOnce({
      entries: [{ id: "", alias: "web1", host: "10.0.0.1", user: "root", auth_kind: "password" }],
      skipped: null,
      note: "Only fields atterm uses are imported",
    });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await openConfigImportDrawer(wrapper);
    expect(wrapper.find('[data-test="ssh-config-import-error"]').exists()).toBe(false);
    expect(wrapper.text()).toContain("web1");
    expect(wrapper.find('[data-test="ssh-config-import-skipped"]').exists()).toBe(false);
  });

  it("entries 为 null 时展示空态而不是抛异常", async () => {
    previewSSHConfigImport.mockResolvedValueOnce({
      entries: null,
      skipped: null,
      note: "Only fields atterm uses are imported",
    });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await openConfigImportDrawer(wrapper);
    expect(wrapper.find('[data-test="ssh-config-import-empty"]').exists()).toBe(true);
  });

  it("只有 proxy_command 的条目标记 ProxyCommand,不说 ProxyJump", async () => {
    previewSSHConfigImport.mockResolvedValueOnce({
      entries: [
        {
          id: "", alias: "corky", host: "10.0.0.9", user: "root", auth_kind: "password",
          proxy_command: "corkscrew proxy 8080 %h %p",
        },
      ],
      skipped: [],
      note: "n",
    });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await openConfigImportDrawer(wrapper);
    const badge = wrapper.find('[data-test="ssh-config-entry-proxy-0"]');
    expect(badge.exists()).toBe(true);
    expect(badge.text()).toContain("ProxyCommand");
    expect(badge.text()).not.toContain("ProxyJump");
  });

  // The two-step import exists because import overwrites hosts that sync to
  // every other device — so which rows overwrite and which create has to be
  // visible before the user commits, not discoverable afterwards.
  it("alias 与已存主机同名的条目标记为覆盖,其余不标", async () => {
    listSSHHosts.mockResolvedValue([
      { id: "1", alias: "web1", host: "10.0.0.1", user: "root", auth_kind: "password" },
    ]);
    previewSSHConfigImport.mockResolvedValueOnce({
      entries: [
        { id: "", alias: "web1", host: "10.0.0.9", user: "root", auth_kind: "password" },
        { id: "", alias: "brand-new", host: "10.0.0.8", user: "root", auth_kind: "password" },
      ],
      skipped: [],
      note: "n",
    });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await openConfigImportDrawer(wrapper);
    expect(wrapper.find('[data-test="ssh-config-entry-overwrite-0"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="ssh-config-entry-overwrite-1"]').exists()).toBe(false);
  });

  // A confirm failure leaves the preview intact, so the entry list must stay
  // on screen — the error belongs in the footer next to the button that
  // failed, not in place of the list the user is still working with.
  it("确认导入失败时错误显示在底部,条目列表不消失", async () => {
    previewSSHConfigImport.mockResolvedValueOnce({
      entries: [{ id: "", alias: "web1", host: "10.0.0.1", user: "root", auth_kind: "password" }],
      skipped: [],
      note: "n",
    });
    importSSHHosts.mockRejectedValueOnce(new Error("config store not ready"));
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await openConfigImportDrawer(wrapper);
    await wrapper.find('[data-test="ssh-config-entry-check-0"]').trigger("change");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="ssh-config-import-confirm"]').trigger("click");
    await flushPromises();
    const footErr = wrapper.find('[data-test="ssh-config-import-confirm-error"]');
    expect(footErr.exists()).toBe(true);
    expect(footErr.text()).toContain("config store not ready");
    expect(wrapper.find('[data-test="ssh-config-import-error"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="ssh-config-entry-check-0"]').exists()).toBe(true);
  });
});

describe("SshHostsPanel 代理主机与导入字段的可见性", () => {
  const PROXIED = {
    id: "p1", alias: "inner", host: "10.0.0.9", user: "root", auth_kind: "password",
    identity_file: "~/.ssh/id_ed25519",
    proxy_jump: "bastion",
  };

  // The regression this pins: the drawer used to build its save payload from
  // the form refs alone, so identity_file / proxy_jump / proxy_command were
  // sent as absent and wiped. Import writes no credential by design, which
  // makes "open this drawer and attach one" the *mandated* next step for an
  // imported host — i.e. the fix must survive exactly the flow that used to
  // strip the not-directly-connectable gate and sync the result everywhere.
  it("编辑代理主机保存时,proxy_jump / identity_file 原样带回后端", async () => {
    listSSHHosts.mockResolvedValue([{ ...PROXIED, proxy_command: "ssh -W %h:%p bastion" }]);
    updateSSHHost.mockResolvedValue(undefined);
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await wrapper.find('[data-test="ssh-host-card-p1"]').trigger("contextmenu");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="session-row-menu-item-edit"]').trigger("click");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="ssh-add-user"]').setValue("deploy");
    await wrapper.find('[data-test="ssh-add-submit"]').trigger("click");
    await flushPromises();
    expect(updateSSHHost).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "p1",
        user: "deploy",
        proxy_jump: "bastion",
        proxy_command: "ssh -W %h:%p bastion",
        identity_file: "~/.ssh/id_ed25519",
      }),
      null,
    );
  });

  it("代理主机在列表里带标记,Connect 被禁用且不会发起连接", async () => {
    listSSHHosts.mockResolvedValue([PROXIED]);
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    const marker = wrapper.find('[data-test="ssh-host-proxy-p1"]');
    expect(marker.exists()).toBe(true);
    expect(marker.text()).toContain("Jump host");
    const btn = wrapper.find('[data-test="ssh-connect-p1"]');
    expect((btn.element as HTMLButtonElement).disabled).toBe(true);
    // Double-click bypasses the disabled button, so the guard has to live in
    // connect() too — a proxied host must never be dialled.
    await wrapper.find('[data-test="ssh-host-card-p1"]').trigger("dblclick");
    await flushPromises();
    expect(newSshSessionByID).not.toHaveBeenCalled();
    expect(wrapper.find('[data-test="ssh-hosts-error"]').text()).toContain("ProxyJump");
  });

  it("只有 proxy_command 的主机,提示说 ProxyCommand 而不是 ProxyJump", async () => {
    listSSHHosts.mockResolvedValue([
      {
        id: "p2", alias: "corky", host: "10.0.0.7", user: "root", auth_kind: "password",
        proxy_command: "corkscrew proxy 8080 %h %p",
      },
    ]);
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    const marker = wrapper.find('[data-test="ssh-host-proxy-p2"]');
    expect(marker.text()).toContain("ProxyCommand");
    await wrapper.find('[data-test="ssh-host-card-p2"]').trigger("dblclick");
    await flushPromises();
    const err = wrapper.find('[data-test="ssh-hosts-error"]').text();
    expect(err).toContain("ProxyCommand");
    expect(err).not.toContain("ProxyJump");
  });

  // §5.2 records IdentityFile as a path precisely so the user knows which key
  // to go import; a path nothing ever renders is a path that never lands.
  it("编辑抽屉展示 identity_file 作为只读提示", async () => {
    listSSHHosts.mockResolvedValue([PROXIED]);
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await wrapper.find('[data-test="ssh-host-card-p1"]').trigger("contextmenu");
    await wrapper.vm.$nextTick();
    await wrapper.find('[data-test="session-row-menu-item-edit"]').trigger("click");
    await wrapper.vm.$nextTick();
    const hint = wrapper.find('[data-test="ssh-host-identity-file"]');
    expect(hint.exists()).toBe(true);
    expect(hint.text()).toContain("~/.ssh/id_ed25519");
    expect(wrapper.find('[data-test="ssh-host-drawer-proxy"]').exists()).toBe(true);
  });

  it("普通主机不带代理标记,Connect 正常工作", async () => {
    listSSHHosts.mockResolvedValue([
      { id: "n1", alias: "plain", host: "10.0.0.1", user: "root", auth_kind: "password" },
    ]);
    newSshSessionByID.mockResolvedValueOnce({ session_id: "s9" });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    expect(wrapper.find('[data-test="ssh-host-proxy-n1"]').exists()).toBe(false);
    await wrapper.find('[data-test="ssh-connect-n1"]').trigger("click");
    await flushPromises();
    expect(newSshSessionByID).toHaveBeenCalledWith("n1");
  });
});

// ---- Port forwarding (roadmap item 26) ----------------------------------

type RuleLike = {
  id: string;
  kind: string;
  bind_addr?: string;
  bind_port: string;
  target_host?: string;
  target_port?: string;
  note?: string;
};

const PG_RULE: RuleLike = {
  id: "f1", kind: "local", bind_addr: "127.0.0.1", bind_port: "5432",
  target_host: "db.internal", target_port: "5432", note: "pg",
};

const FWD_HOST = {
  id: "h1", alias: "box", host: "10.0.0.1", user: "root", port: "22",
  auth_kind: "password" as const, forwards: [PG_RULE],
};

async function mountPanel(hosts: unknown[]) {
  listSSHHosts.mockResolvedValue(hosts);
  const wrapper = mount(SshHostsPanel);
  await flushPromises();
  return wrapper;
}

// Open a host's edit drawer through the context menu, the same path a user
// takes to reach the forwarding editor.
async function openHostDrawer(wrapper: ReturnType<typeof mount>, id: string) {
  await wrapper.find(`[data-test="ssh-host-card-${id}"]`).trigger("contextmenu");
  await wrapper.vm.$nextTick();
  await wrapper.find('[data-test="session-row-menu-item-edit"]').trigger("click");
  await wrapper.vm.$nextTick();
}

function savedForwards(): RuleLike[] {
  const [payload] = updateSSHHost.mock.calls[0] as [{ forwards?: RuleLike[] }];
  return payload.forwards ?? [];
}

describe("SshHostsPanel 转发规则编辑器", () => {
  it("编辑主机时抽屉里列出已有的转发规则", async () => {
    const wrapper = await mountPanel([FWD_HOST]);
    await openHostDrawer(wrapper, "h1");
    const row = wrapper.find('[data-test="ssh-forward-rule-f1"]');
    expect(row.exists()).toBe(true);
    expect((row.find('[data-test="ssh-forward-bind-port-f1"]').element as HTMLInputElement).value).toBe("5432");
    expect((row.find('[data-test="ssh-forward-target-host-f1"]').element as HTMLInputElement).value).toBe("db.internal");
  });

  // The regression this pins is the one that ate proxy_jump in item 25: a save
  // payload built from the fields the form thinks it owns, silently dropping a
  // field it does not — and here dropping it means the rule is deleted on
  // every device, because UpdateSSHHost lets the caller's Forwards win.
  it("只改用户名保存时,原有转发规则原样带回后端", async () => {
    updateSSHHost.mockResolvedValue(undefined);
    const wrapper = await mountPanel([FWD_HOST]);
    await openHostDrawer(wrapper, "h1");
    await wrapper.find('[data-test="ssh-add-user"]').setValue("deploy");
    await wrapper.find('[data-test="ssh-add-submit"]').trigger("click");
    await flushPromises();
    expect(updateSSHHost).toHaveBeenCalledTimes(1);
    const rules = savedForwards();
    expect(rules).toHaveLength(1);
    expect(rules[0]).toMatchObject({
      id: "f1", kind: "local", bind_addr: "127.0.0.1", bind_port: "5432",
      target_host: "db.internal", target_port: "5432", note: "pg",
    });
  });

  it("新增一条规则后保存,新规则出现在写回的 forwards 里", async () => {
    updateSSHHost.mockResolvedValue(undefined);
    const wrapper = await mountPanel([FWD_HOST]);
    await openHostDrawer(wrapper, "h1");
    await wrapper.find('[data-test="ssh-forward-add"]').trigger("click");
    await wrapper.vm.$nextTick();
    const rows = wrapper.findAll('[data-test^="ssh-forward-rule-"]');
    expect(rows).toHaveLength(2);
    // The new row's id is generated, so address it through the row itself.
    const fresh = rows[1];
    await fresh.find('input[data-test^="ssh-forward-bind-port-"]').setValue("6379");
    await fresh.find('input[data-test^="ssh-forward-target-host-"]').setValue("redis.internal");
    await fresh.find('input[data-test^="ssh-forward-target-port-"]').setValue("6379");
    await wrapper.find('[data-test="ssh-add-submit"]').trigger("click");
    await flushPromises();
    const rules = savedForwards();
    expect(rules).toHaveLength(2);
    expect(rules[1]).toMatchObject({
      kind: "local", bind_port: "6379", target_host: "redis.internal", target_port: "6379",
    });
    expect(rules[1].id).toBeTruthy();
  });

  it("删除一条规则后保存,该规则不再写回", async () => {
    updateSSHHost.mockResolvedValue(undefined);
    const wrapper = await mountPanel([FWD_HOST]);
    await openHostDrawer(wrapper, "h1");
    await wrapper.find('[data-test="ssh-forward-remove-f1"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[data-test="ssh-forward-rule-f1"]').exists()).toBe(false);
    await wrapper.find('[data-test="ssh-add-submit"]').trigger("click");
    await flushPromises();
    expect(savedForwards()).toEqual([]);
  });

  it("默认 loopback 绑定不出现警示", async () => {
    const wrapper = await mountPanel([FWD_HOST]);
    await openHostDrawer(wrapper, "h1");
    expect(wrapper.find('[data-test="ssh-forward-bind-warning-f1"]').exists()).toBe(false);
  });

  // §5.1: a non-loopback bind is not a convenience setting, so the UI has to
  // say what it actually costs — in the user's words, not "0.0.0.0".
  it("本地转发绑非 loopback 时警示同网段任何人无需凭据即可访问", async () => {
    const wrapper = await mountPanel([FWD_HOST]);
    await openHostDrawer(wrapper, "h1");
    await wrapper.find('[data-test="ssh-forward-bind-addr-f1"]').setValue("0.0.0.0");
    await wrapper.vm.$nextTick();
    const warn = wrapper.find('[data-test="ssh-forward-bind-warning-f1"]');
    expect(warn.exists()).toBe(true);
    expect(warn.text()).toContain("anyone on the same network");
    expect(warn.text()).toContain("no SSH credential");
    // A local rule exposes one service; it must not be described as a proxy.
    expect(warn.text()).not.toContain("open proxy");
  });

  // §5.4: the same bind on a dynamic rule is a different size of mistake —
  // an unauthenticated SOCKS5 proxy into everything the SSH host can reach.
  it("动态转发绑非 loopback 时额外警示这是无认证的开放代理", async () => {
    const wrapper = await mountPanel([
      { ...FWD_HOST, forwards: [{ id: "d1", kind: "dynamic", bind_addr: "0.0.0.0", bind_port: "1080" }] },
    ]);
    await openHostDrawer(wrapper, "h1");
    const warn = wrapper.find('[data-test="ssh-forward-bind-warning-d1"]');
    expect(warn.exists()).toBe(true);
    expect(warn.text()).toContain("open proxy");
    expect(warn.text()).toContain("anyone on the same network");
    expect(warn.text()).toContain("everything this SSH host can reach");
  });

  it("动态转发不显示目标主机字段", async () => {
    const wrapper = await mountPanel([
      { ...FWD_HOST, forwards: [{ id: "d1", kind: "dynamic", bind_addr: "127.0.0.1", bind_port: "1080" }] },
    ]);
    await openHostDrawer(wrapper, "h1");
    expect(wrapper.find('[data-test="ssh-forward-target-host-d1"]').exists()).toBe(false);
    await wrapper.find('[data-test="ssh-forward-kind-local-d1"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[data-test="ssh-forward-target-host-d1"]').exists()).toBe(true);
  });
});

describe("SshHostsPanel 活跃隧道面板", () => {
  async function openTunnels(wrapper: ReturnType<typeof mount>) {
    await wrapper.find('[data-test="ssh-tab-tunnels"]').trigger("click");
    await flushPromises();
    await wrapper.vm.$nextTick();
  }

  it("没有任何规则时显示空态", async () => {
    const wrapper = await mountPanel([
      { id: "n1", host: "10.0.0.1", user: "root", auth_kind: "password" },
    ]);
    await openTunnels(wrapper);
    expect(wrapper.find('[data-test="ssh-tunnels-empty"]').exists()).toBe(true);
  });

  it("列出主机的规则并可启动,启动后刷新活跃列表", async () => {
    const wrapper = await mountPanel([FWD_HOST]);
    await openTunnels(wrapper);
    const row = wrapper.find('[data-test="ssh-tunnel-row-h1-f1"]');
    expect(row.exists()).toBe(true);
    expect(wrapper.find('[data-test="ssh-tunnel-state-h1-f1"]').text()).toContain("Not started");
    listActiveForwards.mockResolvedValue([
      {
        host_id: "h1", rule_id: "f1", kind: "local", listen_addr: "127.0.0.1:5432",
        target: "db.internal:5432", conns: 0, started_at: 1, running: true, error: "",
      },
    ]);
    await wrapper.find('[data-test="ssh-tunnel-start-h1-f1"]').trigger("click");
    await flushPromises();
    expect(startForward).toHaveBeenCalledWith("h1", "f1");
    expect(wrapper.find('[data-test="ssh-tunnel-state-h1-f1"]').text()).toContain("Running");
  });

  it("运行中的隧道显示真实监听地址与目标,并给出停止按钮", async () => {
    listActiveForwards.mockResolvedValue([
      {
        host_id: "h1", rule_id: "f1", kind: "local", listen_addr: "127.0.0.1:54321",
        target: "db.internal:5432", conns: 3, started_at: 1, running: true, error: "",
      },
    ]);
    const wrapper = await mountPanel([FWD_HOST]);
    await openTunnels(wrapper);
    const row = wrapper.find('[data-test="ssh-tunnel-row-h1-f1"]');
    expect(row.text()).toContain("127.0.0.1:54321");
    expect(row.text()).toContain("db.internal:5432");
    expect(row.text()).toContain("3");
    expect(wrapper.find('[data-test="ssh-tunnel-stop-h1-f1"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="ssh-tunnel-start-h1-f1"]').exists()).toBe(false);
    await wrapper.find('[data-test="ssh-tunnel-stop-h1-f1"]').trigger("click");
    await flushPromises();
    expect(stopForward).toHaveBeenCalledWith("h1", "f1");
  });

  // ListActiveForwards keeps entries that stopped on their own (Running false,
  // Error set). Rendering one of those as live is the specific mistake the
  // design warns about: the listener is gone, so "Running" would be a lie.
  it("连接断开后的隧道显示 stopped 与原因,而不是 running", async () => {
    listActiveForwards.mockResolvedValue([
      {
        host_id: "h1", rule_id: "f1", kind: "local", listen_addr: "127.0.0.1:5432",
        target: "db.internal:5432", conns: 7, started_at: 1, running: false,
        error: "ssh connection lost (keepalive failed or the peer closed it)",
      },
    ]);
    const wrapper = await mountPanel([FWD_HOST]);
    await openTunnels(wrapper);
    const state = wrapper.find('[data-test="ssh-tunnel-state-h1-f1"]');
    expect(state.text()).toContain("Stopped");
    expect(state.text()).not.toContain("Running");
    expect(state.classes()).toContain("stopped");
    const err = wrapper.find('[data-test="ssh-tunnel-error-h1-f1"]');
    expect(err.exists()).toBe(true);
    expect(err.text()).toContain("keepalive failed");
    // A dead tunnel offers a restart, not a stop.
    expect(wrapper.find('[data-test="ssh-tunnel-stop-h1-f1"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="ssh-tunnel-start-h1-f1"]').exists()).toBe(true);
  });

  it("启动失败时把后端的原因显示出来", async () => {
    startForward.mockRejectedValueOnce(new Error("local port 5432 is already in use (cannot bind 127.0.0.1:5432)"));
    const wrapper = await mountPanel([FWD_HOST]);
    await openTunnels(wrapper);
    await wrapper.find('[data-test="ssh-tunnel-start-h1-f1"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-test="ssh-hosts-error"]').text()).toContain("already in use");
  });

  // §5.2: a proxied host is refused by StartForward. Letting the user click and
  // read the error afterwards is exactly what the badge exists to prevent.
  it("带 proxy_jump 的主机启动按钮禁用,并写明原因", async () => {
    const wrapper = await mountPanel([{ ...FWD_HOST, id: "p1", proxy_jump: "bastion" }]);
    await openTunnels(wrapper);
    const btn = wrapper.find('[data-test="ssh-tunnel-start-p1-f1"]');
    expect((btn.element as HTMLButtonElement).disabled).toBe(true);
    const reason = wrapper.find('[data-test="ssh-tunnel-proxy-reason-p1"]');
    expect(reason.exists()).toBe(true);
    expect(reason.text()).toContain("ProxyJump");
    await btn.trigger("click");
    await flushPromises();
    expect(startForward).not.toHaveBeenCalled();
  });

  it("停止后重新拉取活跃列表,该隧道回到未启动态", async () => {
    listActiveForwards.mockResolvedValue([
      {
        host_id: "h1", rule_id: "f1", kind: "local", listen_addr: "127.0.0.1:5432",
        target: "db.internal:5432", conns: 0, started_at: 1, running: true, error: "",
      },
    ]);
    const wrapper = await mountPanel([FWD_HOST]);
    await openTunnels(wrapper);
    listActiveForwards.mockResolvedValue([]);
    await wrapper.find('[data-test="ssh-tunnel-stop-h1-f1"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-test="ssh-tunnel-state-h1-f1"]').text()).toContain("Not started");
  });

  it("动态转发的行不谎报一个目标地址", async () => {
    const wrapper = await mountPanel([
      { ...FWD_HOST, forwards: [{ id: "d1", kind: "dynamic", bind_addr: "127.0.0.1", bind_port: "1080" }] },
    ]);
    await openTunnels(wrapper);
    const row = wrapper.find('[data-test="ssh-tunnel-row-h1-d1"]');
    expect(row.text()).toContain("127.0.0.1:1080");
    expect(row.text()).toContain("SOCKS5");
  });
});
