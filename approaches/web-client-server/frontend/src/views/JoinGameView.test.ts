import { flushPromises, mount } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import JoinGameView from "./JoinGameView.vue";

const api = vi.hoisted(() => ({ exchangeSession: vi.fn() }));
vi.mock("../api/games", () => api);

function testRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/games/:gameId/join",
        name: "join-game",
        component: JoinGameView,
      },
      {
        path: "/games/:gameId",
        name: "game",
        component: { template: "<p>ready</p>" },
      },
      { path: "/", name: "home", component: { template: "<p>home</p>" } },
    ],
  });
}

describe("JoinGameView", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("exchanges the fragment secret and removes it from the route", async () => {
    api.exchangeSession.mockResolvedValue({ gameId: "game-123", seat: 2 });
    const router = testRouter();
    await router.push("/games/game-123/join#token=player-two-secret");
    await router.isReady();
    mount(JoinGameView, { global: { plugins: [router] } });
    await flushPromises();

    expect(api.exchangeSession).toHaveBeenCalledWith(
      "game-123",
      "player-two-secret",
    );
    expect(router.currentRoute.value.fullPath).toBe("/games/game-123");
  });

  it("shows an error and does not scrub a rejected invitation", async () => {
    api.exchangeSession.mockRejectedValue(new Error("invalid"));
    const router = testRouter();
    await router.push("/games/game-123/join#token=modified");
    await router.isReady();
    const wrapper = mount(JoinGameView, { global: { plugins: [router] } });
    await flushPromises();

    expect(wrapper.get('[role="status"]').text()).toContain(
      "invitation link is invalid",
    );
    expect(router.currentRoute.value.hash).toBe("#token=modified");
  });
});
