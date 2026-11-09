import { expect, test, type Page } from "@playwright/test";

async function expectVersion(page: Page, version: number) {
  await expect(page.locator(".board-footer")).toContainText(
    `State v${version}`,
  );
}

async function playTiles(page: Page, indexes: number[], nextVersion: number) {
  const tiles = page.getByRole("gridcell");
  for (const index of indexes) await tiles.nth(index).click();
  await page.getByRole("button", { name: "Submit word" }).click();
  await expectVersion(page, nextVersion);
}

async function register(page: Page, name: string) {
  await page.goto("/");
  await page.getByLabel("Player name").fill(name);
  await page.getByRole("button", { name: "Enter Letterpress" }).click();
  await expect(page).toHaveURL(/\/lobby$/);
}

test("two isolated players resume, recover from stale state, and finish a game", async ({
  browser,
}) => {
  const playerOneContext = await browser.newContext();
  const playerTwoContext = await browser.newContext();
  const playerOne = await playerOneContext.newPage();
  const playerTwo = await playerTwoContext.newPage();

  await register(playerOne, "E2E Alice");
  await register(playerTwo, "E2E Bob");
  await playerOne.getByRole("button", { name: "Specific player" }).click();
  await playerOne.getByLabel("Player name").fill("E2E Bob");
  await playerOne.getByRole("button", { name: "Search" }).click();
  await playerOne.getByRole("button", { name: "Challenge" }).click();
  await expect(
    playerOne.getByRole("heading", { name: "Sent challenges" }),
  ).toBeVisible();
  await expect(playerOne.getByText(/copy|share|invitation/i)).toHaveCount(0);

  await playerTwo.reload();
  await playerTwo.getByRole("button", { name: "Accept" }).click();
  await expect(playerTwo).toHaveURL(/\/games\//);
  const gameURL = playerTwo.url();
  await playerOne.reload();
  await playerOne.getByRole("link", { name: /E2E Bob/ }).click();
  await expect(playerOne).toHaveURL(gameURL);
  await expectVersion(playerTwo, 0);
  await expectVersion(playerOne, 0);
  expect(await playerOne.getByRole("gridcell").allTextContents()).toEqual(
    await playerTwo.getByRole("gridcell").allTextContents(),
  );
  await expect(
    playerOne.getByText("Player 1", { exact: true }).first(),
  ).toBeVisible();
  await expect(
    playerTwo.getByText("Player 2", { exact: true }).first(),
  ).toBeVisible();

  await playTiles(playerOne, [0, 1, 2], 1);
  await playerTwo.reload();
  await expectVersion(playerTwo, 1);
  await playTiles(playerTwo, [3, 4, 5], 2);

  const staleContext = await browser.newContext({
    storageState: await playerOneContext.storageState(),
  });
  await staleContext.addInitScript(() => {
    window.addEventListener(
      "focus",
      (event) => event.stopImmediatePropagation(),
      true,
    );
  });
  const stalePlayerOne = await staleContext.newPage();
  await stalePlayerOne.goto(gameURL);
  await expectVersion(stalePlayerOne, 2);

  await playerOne.reload();
  await expectVersion(playerOne, 2);
  await playTiles(playerOne, [6, 7, 8], 3);
  await playerTwo.reload();
  await expectVersion(playerTwo, 3);
  await playTiles(playerTwo, [9, 10, 11], 4);

  await stalePlayerOne.getByRole("gridcell").nth(12).click();
  await stalePlayerOne.getByRole("button", { name: "Submit word" }).click();
  await expect(stalePlayerOne.getByRole("alert")).toContainText(
    "has been refreshed",
  );
  await expectVersion(stalePlayerOne, 4);

  await playerTwo.close();
  const resumedPlayerTwo = await playerTwoContext.newPage();
  await resumedPlayerTwo.goto(gameURL);
  await expectVersion(resumedPlayerTwo, 4);
  await expect(resumedPlayerTwo.locator(".score-card--two")).toContainText(
    "You",
  );

  await playerOne.reload();
  await expectVersion(playerOne, 4);
  await playTiles(
    playerOne,
    Array.from({ length: 13 }, (_, index) => index + 12),
    5,
  );
  await expect(
    playerOne.getByRole("heading", { name: "You won" }),
  ).toBeVisible();
  await expect(playerOne.locator(".result-card")).toContainText(
    "Player 1 19 · Player 2 6",
  );

  await resumedPlayerTwo.reload();
  await expectVersion(resumedPlayerTwo, 5);
  await expect(
    resumedPlayerTwo.getByRole("heading", { name: "Player 1 won" }),
  ).toBeVisible();
  await expect(resumedPlayerTwo.locator(".result-card")).toContainText(
    "Player 1 19 · Player 2 6",
  );
  await expect(playerOne.locator(".move-history")).toContainText(
    "Move history (5)",
  );
  await expect(resumedPlayerTwo.locator(".move-history")).toContainText(
    "Move history (5)",
  );

  await staleContext.close();
  await playerOneContext.close();
  await playerTwoContext.close();
});

test("random start mode restores a queued player and allocates one shared game", async ({
  browser,
}) => {
  const firstContext = await browser.newContext();
  const secondContext = await browser.newContext();
  const first = await firstContext.newPage();
  const second = await secondContext.newPage();

  await register(first, "E2E Random One");
  await register(second, "E2E Random Two");
  await first.getByRole("button", { name: "Random player" }).click();
  await first.getByRole("button", { name: "Find a human opponent" }).click();
  await expect(
    first.getByText("Looking for another human player…"),
  ).toBeVisible();
  await first.reload();
  await expect(
    first.getByText("Looking for another human player…"),
  ).toBeVisible();

  await second.getByRole("button", { name: "Random player" }).click();
  await second.getByRole("button", { name: "Find a human opponent" }).click();
  await expect(second).toHaveURL(/\/games\//);
  await expect(first).toHaveURL(/\/games\//, { timeout: 10_000 });
  expect(first.url()).toBe(second.url());
  await expectVersion(first, 0);
  await expectVersion(second, 0);

  await firstContext.close();
  await secondContext.close();
});

test("explicit bot selection allocates, displays, and plays through the shared game", async ({
  page,
}) => {
  await register(page, "E2E Bot Player");
  await page.getByRole("button", { name: "Bot", exact: true }).click();
  const botChoice = page.locator(".bot-choice");
  await expect(botChoice).toContainText("Balanced Bot");
  await expect(botChoice).toContainText("balanced");
  await expect(botChoice).toContainText("full dictionary");
  await expect(botChoice).toContainText("baseline");
  await botChoice.getByRole("button", { name: "Play this bot" }).click();
  await expect(page).toHaveURL(/\/games\//);
  const gameURL = page.url();
  await expectVersion(page, 0);

  await page.goto("/lobby");
  const botSummary = page.getByRole("link", { name: /Balanced Bot/ });
  await expect(botSummary).toContainText(
    "Bot · balanced · full dictionary · baseline",
  );
  await botSummary.click();
  await expect(page).toHaveURL(gameURL);

  await playTiles(page, [0, 1, 2], 1);
  await expect(page.locator(".turn-banner")).toContainText(
    "Balanced Bot is thinking",
  );
  await page.reload();

  await expect
    .poll(
      async () => {
        await page.reload();
        return page.locator(".board-footer").textContent();
      },
      { timeout: 20_000 },
    )
    .toContain("State v2");
  await expect(page.locator(".turn-banner")).toContainText("Your turn");
  await expect(page.locator(".move-history")).toContainText("Move history (2)");
});
