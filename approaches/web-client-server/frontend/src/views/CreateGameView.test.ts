import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";

import CreateGameView from "./CreateGameView.vue";

const api = vi.hoisted(() => ({
  createGame: vi.fn(),
  exchangeSession: vi.fn(),
}));

vi.mock("../api/games", () => api);

describe("CreateGameView", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("creates Player 1's session and exposes a copyable Player 2 invitation", async () => {
    api.createGame.mockResolvedValue({
      gameId: "game-123",
      playerOneToken: "player-one-secret",
      playerTwoToken: "player-two-secret",
    });
    api.exchangeSession.mockResolvedValue({ gameId: "game-123", seat: 1 });
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    const wrapper = mount(CreateGameView, {
      global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } },
    });
    await wrapper.get("button").trigger("click");
    await flushPromises();

    expect(api.exchangeSession).toHaveBeenCalledWith(
      "game-123",
      "player-one-secret",
    );
    const input = wrapper.get<HTMLInputElement>("#invitation-link");
    expect(input.element.value).toBe(
      "http://localhost:3000/games/game-123/join#token=player-two-secret",
    );

    await wrapper.get(".secondary-action").trigger("click");
    await flushPromises();
    expect(writeText).toHaveBeenCalledWith(input.element.value);
    expect(wrapper.get(".secondary-action").text()).toBe("Copied");
  });

  it("renders an actionable error when creation fails", async () => {
    api.createGame.mockRejectedValue(new Error("offline"));
    const wrapper = mount(CreateGameView, {
      global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } },
    });

    await wrapper.get("button").trigger("click");
    await flushPromises();

    expect(wrapper.get('[role="alert"]').text()).toContain(
      "Check your connection and try again",
    );
    expect(wrapper.get("button").text()).toBe("Create game");
  });
});
