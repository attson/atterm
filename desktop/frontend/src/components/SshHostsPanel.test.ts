import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SshHostsPanel from "./SshHostsPanel.vue";

const listSSHHosts = vi.fn();
const addSSHHost = vi.fn();
const updateSSHHost = vi.fn();
const deleteSSHHost = vi.fn();
const newSshSessionByID = vi.fn();
vi.mock("../lib/api", () => ({
  listSSHHosts: (...a: unknown[]) => listSSHHosts(...a),
  addSSHHost: (...a: unknown[]) => addSSHHost(...a),
  updateSSHHost: (...a: unknown[]) => updateSSHHost(...a),
  deleteSSHHost: (...a: unknown[]) => deleteSSHHost(...a),
  newSshSessionByID: (...a: unknown[]) => newSshSessionByID(...a),
}));

beforeEach(() => {
  listSSHHosts.mockReset().mockResolvedValue([]);
  addSSHHost.mockReset();
  updateSSHHost.mockReset();
  deleteSSHHost.mockReset();
  newSshSessionByID.mockReset();
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
});
