import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";

import HomeView from "./HomeView.vue";

describe("HomeView", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders the Letterpress landing page and reports a healthy server", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: "ok" }),
    });
    vi.stubGlobal("fetch", fetchMock);

    const wrapper = mount(HomeView);
    await flushPromises();

    expect(wrapper.get("h1").text()).toBe(
      "Claim the board, one word at a time.",
    );
    expect(wrapper.get('[role="status"]').text()).toBe("Game server connected");
    expect(fetchMock).toHaveBeenCalledWith("/healthz", {
      headers: { Accept: "application/json" },
    });
  });

  it("reports when the server cannot be reached", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")));

    const wrapper = mount(HomeView);
    await flushPromises();

    expect(wrapper.get('[role="status"]').text()).toBe(
      "Game server unavailable",
    );
  });
});
