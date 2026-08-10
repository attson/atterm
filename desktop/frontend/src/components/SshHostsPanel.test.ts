import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SshHostsPanel from "./SshHostsPanel.vue";

const listSSHHosts = vi.fn();
const addSSHHost = vi.fn();
const updateSSHHost = vi.fn();
const deleteSSHHost = vi.fn();
const newSshSessionByID = vi.fn();
const listSSHKeys = vi.fn();
const addSSHKey = vi.fn();
const updateSSHKey = vi.fn();
const deleteSSHKey = vi.fn();
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
}));

beforeEach(() => {
  listSSHHosts.mockReset().mockResolvedValue([]);
  addSSHHost.mockReset();
  updateSSHHost.mockReset();
  deleteSSHHost.mockReset();
  newSshSessionByID.mockReset();
  listSSHKeys.mockReset().mockResolvedValue([]);
  addSSHKey.mockReset();
  updateSSHKey.mockReset();
  deleteSSHKey.mockReset();
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
