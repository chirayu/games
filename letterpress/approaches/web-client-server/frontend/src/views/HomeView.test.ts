import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { resetPlayerSessionForTests } from "../composables/playerSession";
import HomeView from "./HomeView.vue";

const replace = vi.fn();
vi.mock("vue-router", () => ({ useRouter: () => ({ replace }) }));

function response(status: number, body?: unknown) {
  return { ok: status >= 200 && status < 300, status, json: async () => body };
}

describe("HomeView", () => {
  beforeEach(() => {
    resetPlayerSessionForTests();
    replace.mockReset();
    vi.unstubAllGlobals();
  });

  it("registers a new valid name and enters the lobby", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        response(401, {
          error: { code: "player_session_required", message: "Sign in" },
        }),
      )
      .mockResolvedValueOnce(
        response(201, { data: { id: "p1", name: "Blue Fox" } }),
      );
    vi.stubGlobal("fetch", fetchMock);
    const wrapper = mount(HomeView);
    await flushPromises();

    await wrapper.get("input").setValue("  Blue   Fox  ");
    await wrapper.get("form").trigger("submit");
    await flushPromises();

    expect(fetchMock).toHaveBeenLastCalledWith(
      "/api/v1/player-sessions",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ name: "Blue Fox" }),
      }),
    );
    expect(replace).toHaveBeenCalledWith({ name: "lobby" });
  });

  it("logs in with an existing name through the same entry flow", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        response(401, { error: { code: "player_session_required" } }),
      )
      .mockResolvedValueOnce(
        response(200, { data: { id: "existing", name: "Alice" } }),
      );
    vi.stubGlobal("fetch", fetchMock);
    const wrapper = mount(HomeView);
    await flushPromises();
    await wrapper.get("input").setValue("Alice");
    await wrapper.get("form").trigger("submit");
    await flushPromises();

    expect(replace).toHaveBeenCalledWith({ name: "lobby" });
  });

  it("restores an existing session without showing name entry", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          response(200, { data: { id: "p1", name: "Alice" } }),
        ),
    );
    mount(HomeView);
    await flushPromises();
    expect(replace).toHaveBeenCalledWith({ name: "lobby" });
  });

  it("validates names before submitting", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        response(401, { error: { code: "player_session_required" } }),
      );
    vi.stubGlobal("fetch", fetchMock);
    const wrapper = mount(HomeView);
    await flushPromises();
    await wrapper.get("input").setValue("No!");
    await wrapper.get("form").trigger("submit");

    expect(wrapper.get('[role="alert"]').text()).toContain("Use 1–24");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("shows an actionable network failure and the name-sharing warning", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")));
    const wrapper = mount(HomeView);
    await flushPromises();

    expect(wrapper.get('[role="alert"]').text()).toContain(
      "Check your connection",
    );
    expect(wrapper.get(".identity-warning").text()).toContain(
      "Anyone who enters the same name",
    );
    expect(wrapper.get("button").text()).toBe("Try again");
  });
});
