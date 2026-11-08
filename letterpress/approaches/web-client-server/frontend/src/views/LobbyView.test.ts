import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  resetPlayerSessionForTests,
  startPlayerSession,
} from "../composables/playerSession";
import LobbyView from "./LobbyView.vue";

const replace = vi.fn();
vi.mock("vue-router", () => ({ useRouter: () => ({ replace }) }));

describe("LobbyView", () => {
  beforeEach(async () => {
    resetPlayerSessionForTests();
    replace.mockReset();
    vi.unstubAllGlobals();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ data: { id: "p1", name: "Alice" } }),
      }),
    );
    await startPlayerSession("Alice");
  });

  it("always shows the current player and logout", () => {
    const wrapper = mount(LobbyView);
    expect(wrapper.text()).toContain("Signed in as Alice");
    expect(wrapper.get("button").text()).toBe("Log out");
  });

  it("logs out the current browser session and returns to name entry", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 204,
        json: async () => undefined,
      }),
    );
    const wrapper = mount(LobbyView);
    await wrapper.get("button").trigger("click");
    await flushPromises();

    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/player-sessions/current",
      expect.objectContaining({ method: "DELETE" }),
    );
    expect(replace).toHaveBeenCalledWith({ name: "home" });
  });
});
