import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import NewSshDialog from "./NewSshDialog.vue";

const newSshSession = vi.fn();
vi.mock("../lib/api", () => ({
  newSshSession: (...a: unknown[]) => newSshSession(...a),
}));

beforeEach(() => newSshSession.mockReset());

async function fillBasics(wrapper: ReturnType<typeof mount>) {
  await wrapper.find('[data-test="ssh-host"]').setValue("h");
  await wrapper.find('[data-test="ssh-user"]').setValue("u");
  await wrapper.find('[data-test="ssh-password"]').setValue("pw");
}

describe("NewSshDialog", () => {
  it("提交时用表单字段调用 newSshSession", async () => {
    newSshSession.mockResolvedValue({ session_id: "s1" });
    const wrapper = mount(NewSshDialog);
    await fillBasics(wrapper);
    await wrapper.find('[data-test="ssh-connect"]').trigger("click");
    expect(newSshSession).toHaveBeenCalledWith(
      expect.objectContaining({ host: "h", user: "u", password: "pw", auth_kind: "password" }),
    );
  });

  it("成功后 emit connected 携带 session id", async () => {
    newSshSession.mockResolvedValue({ session_id: "s1" });
    const wrapper = mount(NewSshDialog);
    await fillBasics(wrapper);
    await wrapper.find('[data-test="ssh-connect"]').trigger("click");
    await flushPromises();
    expect(wrapper.emitted("connected")?.[0]).toEqual(["s1"]);
  });

  it("未知主机错误时展示指纹,确认后带 accept_host_key 重试", async () => {
    newSshSession
      .mockRejectedValueOnce({ Fingerprint: "SHA256:abc", Host: "h" })
      .mockResolvedValueOnce({ session_id: "s1" });
    const wrapper = mount(NewSshDialog);
    await fillBasics(wrapper);
    await wrapper.find('[data-test="ssh-connect"]').trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("SHA256:abc");

    await wrapper.find('[data-test="ssh-accept-hostkey"]').trigger("click");
    await flushPromises();
    expect(newSshSession).toHaveBeenLastCalledWith(
      expect.objectContaining({ accept_host_key: true }),
    );
  });

  it("认证失败等普通错误展示错误信息且不重试", async () => {
    // A non-Error rejection (matches how Wails surfaces Go errors as strings)
    // avoids vitest's unhandled-Error-rejection heuristic while still
    // exercising the generic error branch.
    newSshSession.mockRejectedValueOnce("auth failed");
    const wrapper = mount(NewSshDialog);
    await fillBasics(wrapper);
    await wrapper.find('[data-test="ssh-connect"]').trigger("click");
    await flushPromises();
    expect(wrapper.text().toLowerCase()).toContain("auth failed");
    expect(wrapper.find('[data-test="ssh-accept-hostkey"]').exists()).toBe(false);
  });
});
