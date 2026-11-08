import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  resetPlayerSessionForTests,
  startPlayerSession,
} from "../composables/playerSession";
import LobbyView from "./LobbyView.vue";

const replace = vi.fn();
const push = vi.fn();
vi.mock("vue-router", () => ({ useRouter: () => ({ replace, push }) }));

function response(status: number, body?: unknown) {
  return { ok: status >= 200 && status < 300, status, json: async () => body };
}

function mountLobby() {
  return mount(LobbyView, {
    global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } },
  });
}

async function establishAlice(fetchMock: ReturnType<typeof vi.fn>) {
  vi.stubGlobal("fetch", fetchMock);
  await startPlayerSession("Alice");
}

const activeGames = {
  data: {
    items: [
      {
        id: "your-turn",
        opponent: { id: "p2", name: "Bob", type: "human" },
        playerSeat: 1,
        activePlayer: 1,
        status: "active",
        scores: { playerOne: 4, playerTwo: 2 },
        version: 3,
        lastActivity: "2026-11-08T12:00:00Z",
      },
      {
        id: "waiting",
        opponent: { id: "p3", name: "Carol", type: "human" },
        playerSeat: 2,
        activePlayer: 1,
        status: "active",
        scores: { playerOne: 5, playerTwo: 6 },
        version: 4,
        lastActivity: "2026-11-08T11:00:00Z",
      },
    ],
  },
};

describe("LobbyView", () => {
  beforeEach(() => {
    resetPlayerSessionForTests();
    replace.mockReset();
    push.mockReset();
    vi.unstubAllGlobals();
  });

  it("groups active games by turn and links to the authoritative game", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        response(200, { data: { id: "p1", name: "Alice" } }),
      )
      .mockResolvedValueOnce(response(200, activeGames))
      .mockResolvedValueOnce(response(200, { data: { items: [] } }));
    await establishAlice(fetchMock);
    const wrapper = mountLobby();
    await flushPromises();

    expect(wrapper.text()).toContain("Signed in as Alice");
    expect(wrapper.text()).toContain("Your turn");
    expect(wrapper.text()).toContain("Waiting for opponent");
    expect(wrapper.text()).toContain("Bob");
    expect(wrapper.text()).toContain("Carol");
    expect(wrapper.findAll(".game-summary")).toHaveLength(2);
  });

  it("keeps the empty lobby useful and returns player IDs from search", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        response(200, { data: { id: "p1", name: "Alice" } }),
      )
      .mockResolvedValueOnce(response(200, { data: { items: [] } }))
      .mockResolvedValueOnce(response(200, { data: { items: [] } }))
      .mockResolvedValueOnce(
        response(200, {
          data: { items: [{ id: "p2", name: "Blue Fox", type: "human" }] },
        }),
      );
    await establishAlice(fetchMock);
    const wrapper = mountLobby();
    await flushPromises();
    expect(wrapper.text()).toContain("No active games yet");

    await wrapper.get("#player-search").setValue("blue");
    await wrapper.get(".search-form").trigger("submit");
    await flushPromises();
    expect(wrapper.get('[data-player-id="p2"]').text()).toContain("Blue Fox");
  });

  it("shows stale-session and transient failure states", async () => {
    const staleFetch = vi
      .fn()
      .mockResolvedValueOnce(
        response(200, { data: { id: "p1", name: "Alice" } }),
      )
      .mockResolvedValueOnce(
        response(401, { error: { code: "player_session_required" } }),
      )
      .mockResolvedValueOnce(response(200, { data: { items: [] } }));
    await establishAlice(staleFetch);
    const stale = mountLobby();
    await flushPromises();
    expect(stale.text()).toContain("Your session has expired");

    resetPlayerSessionForTests();
    const failedFetch = vi
      .fn()
      .mockResolvedValueOnce(
        response(200, { data: { id: "p1", name: "Alice" } }),
      )
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce(response(200, { data: { items: [] } }));
    await establishAlice(failedFetch);
    const failed = mountLobby();
    await flushPromises();
    expect(failed.text()).toContain("Your games couldn’t be loaded");
    expect(failed.text()).toContain("Try again");
  });

  it("accepts an incoming challenge and opens the allocated game", async () => {
    const challenge = {
      allocationId: "allocation-1",
      direction: "incoming",
      requester: { id: "p2", name: "Bob", type: "human" },
      opponent: { id: "p1", name: "Alice", type: "human" },
      expiresAt: "2026-11-15T12:00:00Z",
      createdAt: "2026-11-08T12:00:00Z",
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        response(200, { data: { id: "p1", name: "Alice" } }),
      )
      .mockResolvedValueOnce(response(200, { data: { items: [] } }))
      .mockResolvedValueOnce(response(200, { data: { items: [challenge] } }))
      .mockResolvedValueOnce(
        response(200, {
          data: { id: "allocation-1", state: "allocated", gameId: "game-1" },
        }),
      );
    await establishAlice(fetchMock);
    const wrapper = mountLobby();
    await flushPromises();

    expect(wrapper.text()).toContain("Incoming challenges");
    await wrapper.get(".challenge-list .primary-action").trigger("click");
    await flushPromises();

    expect(fetchMock).toHaveBeenLastCalledWith(
      "/api/v1/game-allocations/allocation-1/accept",
      expect.objectContaining({ method: "POST" }),
    );
    expect(push).toHaveBeenCalledWith("/games/game-1");
  });

  it("starts random human matchmaking and opens an immediate match", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        response(200, { data: { id: "p1", name: "Alice" } }),
      )
      .mockResolvedValueOnce(response(200, { data: { items: [] } }))
      .mockResolvedValueOnce(response(200, { data: { items: [] } }))
      .mockResolvedValueOnce(
        response(201, {
          data: {
            id: "random-1",
            mode: "random",
            allowedOpponentTypes: ["human"],
            state: "allocated",
            gameId: "random-game",
          },
        }),
      );
    await establishAlice(fetchMock);
    const wrapper = mountLobby();
    await flushPromises();

    await wrapper.get(".random-matchmaking .primary-action").trigger("click");
    await flushPromises();

    const [path, request] = fetchMock.mock.calls.at(-1)!;
    expect(path).toBe("/api/v1/game-allocations");
    expect(request).toEqual(expect.objectContaining({ method: "POST" }));
    expect(JSON.parse(request.body)).toEqual({
      mode: "random",
      allowedOpponentTypes: ["human"],
    });
    expect(push).toHaveBeenCalledWith("/games/random-game");
  });

  it("shows and cancels a pending random queue entry", async () => {
    const queued = {
      id: "random-queued",
      mode: "random",
      allowedOpponentTypes: ["human"],
      state: "queued",
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        response(200, { data: { id: "p1", name: "Alice" } }),
      )
      .mockResolvedValueOnce(response(200, { data: { items: [] } }))
      .mockResolvedValueOnce(response(200, { data: { items: [] } }))
      .mockResolvedValueOnce(response(201, { data: queued }))
      .mockResolvedValueOnce(
        response(200, { data: { ...queued, state: "cancelled" } }),
      );
    await establishAlice(fetchMock);
    const wrapper = mountLobby();
    await flushPromises();

    await wrapper.get(".random-matchmaking .primary-action").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("Looking for another human player");
    await wrapper.get(".random-matchmaking .secondary-action").trigger("click");
    await flushPromises();

    expect(fetchMock).toHaveBeenLastCalledWith(
      "/api/v1/game-allocations/random-queued",
      expect.objectContaining({ method: "DELETE" }),
    );
    expect(wrapper.text()).toContain("Find a human opponent");
    wrapper.unmount();
  });

  it("logs out the current browser session and returns to name entry", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        response(200, { data: { id: "p1", name: "Alice" } }),
      )
      .mockResolvedValueOnce(response(200, { data: { items: [] } }))
      .mockResolvedValueOnce(response(200, { data: { items: [] } }))
      .mockResolvedValueOnce(response(204));
    await establishAlice(fetchMock);
    const wrapper = mountLobby();
    await flushPromises();
    await wrapper.get(".lobby-header button").trigger("click");
    await flushPromises();

    expect(fetchMock).toHaveBeenLastCalledWith(
      "/api/v1/player-sessions/current",
      expect.objectContaining({ method: "DELETE" }),
    );
    expect(replace).toHaveBeenCalledWith({ name: "home" });
  });
});
