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

  // The retry must echo back *both* halves of what the user was shown, exactly
  // as they arrived. A bare "accept the next unknown key" bool is what this
  // replaced: KnownHostsCallback persists an accepted key, so a blanket accept
  // would record keys for machines the user never saw. Rebuilding either half
  // from the form fields is the same bug wearing a different hat — the form's
  // host is "h", but the key is scoped to whatever known_hosts name the backend
  // reported (here "[h]:2222"), and a reconstructed value would silently stop
  // matching.
  it("未知主机错误时展示指纹,确认后原样回带 host + fingerprint", async () => {
    newSshSession
      .mockRejectedValueOnce({ Fingerprint: "SHA256:abc", Host: "[h]:2222", HopIndex: 0, HopName: "" })
      .mockResolvedValueOnce({ session_id: "s1" });
    const wrapper = mount(NewSshDialog);
    await fillBasics(wrapper);
    await wrapper.find('[data-test="ssh-connect"]').trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("SHA256:abc");

    await wrapper.find('[data-test="ssh-accept-hostkey"]').trigger("click");
    await flushPromises();
    const sent = newSshSession.mock.calls.at(-1)?.[0];
    expect(sent.accepted_host_key_host).toBe("[h]:2222");
    expect(sent.accepted_host_key_fingerprint).toBe("SHA256:abc");
    // The field Go stopped reading must not be sent at all: leaving it in place
    // is exactly the state that made this dialog re-prompt forever.
    expect(sent.accept_host_key).toBeUndefined();
  });

  // The ad-hoc dialog never builds a chain (no saved host, so HopIndex is
  // always 0), and its prompt must keep reading the way it always has — no hop
  // numbers, no talk of jump hosts.
  it("直连主机的 TOFU 文案不提跳数,和加跳板链路之前一致", async () => {
    newSshSession.mockRejectedValueOnce({ Fingerprint: "SHA256:abc", Host: "h", HopIndex: 0, HopName: "" });
    const wrapper = mount(NewSshDialog);
    await fillBasics(wrapper);
    await wrapper.find('[data-test="ssh-connect"]').trigger("click");
    await flushPromises();
    const tofu = wrapper.find('[data-test="ssh-tofu"]');
    expect(tofu.text()).toContain("Unknown host key. Verify this fingerprint before trusting it:");
    expect(tofu.text().toLowerCase()).not.toContain("hop");
    expect(tofu.text().toLowerCase()).not.toContain("jump");
  });

  // An error carrying a fingerprint but no host cannot be answered: echoing a
  // fingerprint without the machine it belongs to matches nothing, so the retry
  // would prompt again forever. Show it as a plain error instead.
  it("只有指纹没有 host 的错误不当作 TOFU 处理", async () => {
    newSshSession.mockRejectedValueOnce({ Fingerprint: "SHA256:abc", Host: "" });
    const wrapper = mount(NewSshDialog);
    await fillBasics(wrapper);
    await wrapper.find('[data-test="ssh-connect"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-test="ssh-accept-hostkey"]').exists()).toBe(false);
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
