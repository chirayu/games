import { createRouter, createWebHistory } from "vue-router";

import { restorePlayerSession } from "../composables/playerSession";
import HomeView from "../views/HomeView.vue";
import GameView from "../views/GameView.vue";
import LobbyView from "../views/LobbyView.vue";

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: "/",
      name: "home",
      component: HomeView,
      meta: { guestOnly: true },
    },
    {
      path: "/lobby",
      name: "lobby",
      component: LobbyView,
      meta: { requiresAuth: true },
    },
    {
      path: "/games/:gameId",
      name: "game",
      component: GameView,
      meta: { requiresAuth: true },
    },
  ],
});

router.beforeEach(async (to) => {
  const player = await restorePlayerSession();
  if (to.meta.requiresAuth && !player) return { name: "home" };
  if (to.meta.guestOnly && player) return { name: "lobby" };
});

export default router;
