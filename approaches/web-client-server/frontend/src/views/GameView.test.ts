import { flushPromises, mount } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { GameState } from "../api/games";
import GameView from "./GameView.vue";

const api = vi.hoisted(() => ({ getGame: vi.fn(), submitMove: vi.fn() }));
vi.mock("../api/games", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/games")>()),
  getGame: api.getGame,
  submitMove: api.submitMove,
}));

function testRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/games/:gameId", name: "game", component: GameView },
      { path: "/", name: "home", component: { template: "<p>home</p>" } },
    ],
  });
}

async function mountGame() {
  const router = testRouter();
  await router.push("/games/game-123");
  await router.isReady();
  return {
    router,
    wrapper: mount(GameView, { global: { plugins: [router] } }),
  };
}

function gameState(): GameState {
  return {
    id: "game-123",
    width: 5,
    height: 5,
    tiles: Array.from({ length: 25 }, (_, index) => ({
      index,
      letter: String.fromCharCode(65 + (index % 26)),
      owner: index === 0 ? 1 : index === 24 ? 2 : 0,
      protected: index === 0,
    })),
    scores: { playerOne: 1, playerTwo: 1 },
    activePlayer: 1,
    playerSeat: 1,
    version: 3,
    status: "active",
  };
}

describe("GameView", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("renders the authoritative board, scores, perspective, and accessible tiles", async () => {
    api.getGame.mockResolvedValue(gameState());
    const { wrapper } = await mountGame();
    await flushPromises();

    expect(api.getGame).toHaveBeenCalledWith("game-123");
    expect(wrapper.get(".turn-banner").text()).toBe("Your turn");
    expect(wrapper.findAll('[role="gridcell"]')).toHaveLength(25);
    const firstTile = wrapper.get('[role="gridcell"]');
    expect(firstTile.element.tagName).toBe("BUTTON");
    expect(firstTile.attributes("aria-label")).toContain(
      "A, owned by Player 1, protected, row 1, column 1",
    );
    expect(firstTile.find(".protection-mark").exists()).toBe(true);
    expect(wrapper.get(".score-card--one").text()).toContain("You");
    expect(wrapper.get(".board-footer").text()).toContain("State v3");
  });

  it("shows a loading state while fetching on page load", async () => {
    api.getGame.mockReturnValue(new Promise(() => undefined));
    const { wrapper } = await mountGame();
    expect(wrapper.get('[role="status"]').text()).toContain(
      "Loading the board",
    );
  });

  it("labels the authenticated Player 2 perspective", async () => {
    const state = gameState();
    state.playerSeat = 2;
    api.getGame.mockResolvedValue(state);
    const { wrapper } = await mountGame();
    await flushPromises();

    expect(wrapper.get(".turn-banner").text()).toBe("Player 1’s turn");
    expect(wrapper.get(".score-card--two").text()).toContain("You");
    expect(wrapper.get(".score-card--one").text()).not.toContain("You");
    expect(
      wrapper.get<HTMLButtonElement>(".move-submit").element.disabled,
    ).toBe(true);
    expect(wrapper.get('[role="gridcell"]').attributes("aria-disabled")).toBe(
      "true",
    );
  });

  it("forms a word in selection order, deselects, and clears", async () => {
    api.getGame.mockResolvedValue(gameState());
    const { wrapper } = await mountGame();
    await flushPromises();
    const tiles = wrapper.findAll('[role="gridcell"]');

    await tiles[2].trigger("click");
    await tiles[0].trigger("click");
    expect(wrapper.get(".move-composer__word strong").text()).toBe("CA");
    expect(tiles[2].get(".selection-order").text()).toBe("1");
    expect(tiles[0].get(".selection-order").text()).toBe("2");

    await tiles[2].trigger("click");
    expect(wrapper.get(".move-composer__word strong").text()).toBe("A");
    await wrapper.get(".move-clear").trigger("click");
    expect(wrapper.get(".move-composer__word strong").text()).toBe(
      "Select tiles",
    );
  });

  it("submits selected indexes once and replaces the board state", async () => {
    const updated = gameState();
    updated.activePlayer = 2;
    updated.version = 4;
    updated.tiles[2].owner = 1;
    api.getGame.mockResolvedValue(gameState());
    api.submitMove.mockResolvedValue(updated);
    const { wrapper } = await mountGame();
    await flushPromises();
    const tiles = wrapper.findAll('[role="gridcell"]');

    await tiles[2].trigger("click");
    await tiles[0].trigger("click");
    await wrapper.get(".move-submit").trigger("click");
    await flushPromises();

    expect(api.submitMove).toHaveBeenCalledWith("game-123", [2, 0], 3);
    expect(wrapper.get(".turn-banner").text()).toBe("Player 2’s turn");
    expect(wrapper.get(".move-composer__word strong").text()).toBe(
      "Select tiles",
    );
    expect(
      wrapper
        .get('[role="gridcell"][aria-colindex="3"]')
        .attributes("data-owner"),
    ).toBe("1");
  });

  it.each([
    { code: "repeated_word", message: "already been played" },
    { code: "prefix_word", message: "prefix of a word" },
    { code: "out_of_turn", message: "no longer your turn" },
  ])("shows an understandable $code error", async ({ code, message }) => {
    api.getGame.mockResolvedValue(gameState());
    api.submitMove.mockRejectedValue({ code, status: 422 });
    const { wrapper } = await mountGame();
    await flushPromises();

    await wrapper.get('[role="gridcell"]').trigger("click");
    await wrapper.get(".move-submit").trigger("click");
    await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toContain(message);
  });

  it("refreshes authoritative state after a version conflict", async () => {
    const refreshed = gameState();
    refreshed.activePlayer = 2;
    refreshed.version = 4;
    api.getGame
      .mockResolvedValueOnce(gameState())
      .mockResolvedValueOnce(refreshed);
    api.submitMove.mockRejectedValue({ code: "version_conflict", status: 409 });
    const { wrapper } = await mountGame();
    await flushPromises();

    await wrapper.get('[role="gridcell"]').trigger("click");
    await wrapper.get(".move-submit").trigger("click");
    await flushPromises();
    expect(api.getGame).toHaveBeenCalledTimes(2);
    expect(wrapper.get('[role="alert"]').text()).toContain(
      "has been refreshed",
    );
    expect(wrapper.get(".turn-banner").text()).toBe("Player 2’s turn");
  });

  it("locks submission while a move is in flight", async () => {
    let resolveMove: (value: GameState) => void = () => undefined;
    api.getGame.mockResolvedValue(gameState());
    api.submitMove.mockReturnValue(
      new Promise<GameState>((resolve) => {
        resolveMove = resolve;
      }),
    );
    const { wrapper } = await mountGame();
    await flushPromises();
    await wrapper.get('[role="gridcell"]').trigger("click");

    await wrapper.get(".move-submit").trigger("click");
    expect(
      wrapper.get<HTMLButtonElement>(".move-submit").element.disabled,
    ).toBe(true);
    await wrapper.get(".move-submit").trigger("click");
    expect(api.submitMove).toHaveBeenCalledTimes(1);
    resolveMove(gameState());
    await flushPromises();
  });

  it.each([
    { status: 401, heading: "This seat isn’t available here." },
    { status: 404, heading: "That board doesn’t exist." },
  ])("shows the $status state", async ({ status, heading }) => {
    api.getGame.mockRejectedValue({ status });
    const { wrapper } = await mountGame();
    await flushPromises();
    expect(wrapper.get("h1").text()).toBe(heading);
  });

  it("retries a transient failure", async () => {
    api.getGame
      .mockRejectedValueOnce({ status: 500 })
      .mockResolvedValueOnce(gameState());
    const { wrapper } = await mountGame();
    await flushPromises();
    expect(wrapper.get("h1").text()).toBe("The board couldn’t be loaded.");

    await wrapper.get("button").trigger("click");
    await flushPromises();
    expect(api.getGame).toHaveBeenCalledTimes(2);
    expect(wrapper.findAll('[role="gridcell"]')).toHaveLength(25);
  });
});
