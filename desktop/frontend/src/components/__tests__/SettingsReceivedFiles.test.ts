import { describe, it, expect, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import SettingsReceivedFiles from "../SettingsReceivedFiles.vue";

vi.mock("../../../wailsjs/go/main/App", () => ({
  ReceivedFilesList: vi.fn().mockResolvedValue({
    total_bytes: 350,
    sessions: [
      {
        session_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
        session_name: "shell-1",
        bytes: 300,
        files: [
          { name: "a.txt", bytes: 100, received_at: 1 },
          { name: "b.pdf", bytes: 200, received_at: 2 },
        ],
      },
    ],
  }),
  ReceivedFilesClearAll: vi.fn().mockResolvedValue(undefined),
  ReceivedFilesClearSession: vi.fn().mockResolvedValue(undefined),
  ReceivedFilesDelete: vi.fn().mockResolvedValue(undefined),
  ReceivedFilesOpenDir: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("../../../wailsjs/go/models", () => ({
  main: {},
}));

describe("SettingsReceivedFiles", () => {
  it("renders summary and per-session totals", async () => {
    const w = mount(SettingsReceivedFiles);
    await flushPromises();
    // total-bytes appears in the header
    expect(w.get('[data-testid="total-bytes"]').text()).toContain("350");
    // session name shows
    expect(w.text()).toContain("shell-1");
    // per-session file rows appear only after expand
    expect(w.text()).not.toContain("a.txt");
    await w.get('[data-testid="session-row"]').trigger("click");
    expect(w.text()).toContain("a.txt");
    expect(w.text()).toContain("b.pdf");
  });
});
