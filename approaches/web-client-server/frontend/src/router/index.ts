import { createRouter, createWebHistory } from "vue-router";

import HomeView from "../views/HomeView.vue";
import CreateGameView from "../views/CreateGameView.vue";
import GameReadyView from "../views/GameReadyView.vue";
import JoinGameView from "../views/JoinGameView.vue";

export default createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: "/",
      name: "home",
      component: HomeView,
    },
    {
      path: "/games/new",
      name: "create-game",
      component: CreateGameView,
    },
    {
      path: "/games/:gameId/join",
      name: "join-game",
      component: JoinGameView,
    },
    {
      path: "/games/:gameId",
      name: "game",
      component: GameReadyView,
    },
  ],
});
