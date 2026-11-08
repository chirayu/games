import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  resetPlayerSessionForTests,
  startPlayerSession,
} from "../composables/playerSession";
import LobbyView from "./LobbyView.vue";

const replace = vi.fn();
vi.mock("vue-router", () => ({ useRouter: () => ({ replace }) }));

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
    vi.unstubAllGlobals();
  });

  it("groups active games by turn and links to the authoritative game", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        response(200, { data: { id: "p1", name: "Alice" } }),
      )
      .mockResolvedValueOnce(response(200, activeGames));
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
      );
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
      .mockRejectedValueOnce(new Error("offline"));
    await establishAlice(failedFetch);
    const failed = mountLobby();
    await flushPromises();
    expect(failed.text()).toContain("Your games couldn’t be loaded");
    expect(failed.text()).toContain("Try again");
  });

  it("logs out the current browser session and returns to name entry", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        response(200, { data: { id: "p1", name: "Alice" } }),
      )
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
