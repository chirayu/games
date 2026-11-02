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

test("two isolated players resume, recover from stale state, and finish a game", async ({
  browser,
}) => {
  const playerOneContext = await browser.newContext({
    permissions: ["clipboard-read", "clipboard-write"],
  });
  const playerTwoContext = await browser.newContext();
  const playerOne = await playerOneContext.newPage();
  const playerTwo = await playerTwoContext.newPage();

  await playerOne.goto("/games/new");
  await playerOne.getByRole("button", { name: "Create game" }).click();
  const invitation = playerOne.getByLabel("Player 2 invitation");
  await expect(invitation).toBeVisible();
  const invitationURL = await invitation.inputValue();
  await playerOne.getByRole("button", { name: "Copy link" }).click();
  await expect(playerOne.getByRole("button", { name: "Copied" })).toBeVisible();
  await playerOne.getByRole("link", { name: "Continue to your game" }).click();
  await expectVersion(playerOne, 0);
  const gameURL = playerOne.url();

  await playerTwo.goto(invitationURL);
  await expect(playerTwo).toHaveURL(gameURL);
  await expectVersion(playerTwo, 0);
  expect(await playerOne.getByRole("gridcell").allTextContents()).toEqual(
    await playerTwo.getByRole("gridcell").allTextContents(),
  );
  await expect(
    playerOne.getByText("Player 1", { exact: true }).first(),
  ).toBeVisible();
  await expect(
    playerTwo.getByText("Player 2", { exact: true }).first(),
  ).toBeVisible();

  await playTiles(playerOne, [0], 1);
  await playerTwo.reload();
  await expectVersion(playerTwo, 1);
  await playTiles(playerTwo, [1], 2);

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
  await playTiles(playerOne, [2], 3);
  await playerTwo.reload();
  await expectVersion(playerTwo, 3);
  await playTiles(playerTwo, [3], 4);

  await stalePlayerOne.getByRole("gridcell").nth(4).click();
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
    Array.from({ length: 21 }, (_, index) => index + 4),
    5,
  );
  await expect(
    playerOne.getByRole("heading", { name: "You won" }),
  ).toBeVisible();
  await expect(playerOne.locator(".result-card")).toContainText(
    "Player 1 23 · Player 2 2",
  );

  await resumedPlayerTwo.reload();
  await expectVersion(resumedPlayerTwo, 5);
  await expect(
    resumedPlayerTwo.getByRole("heading", { name: "Player 1 won" }),
  ).toBeVisible();
  await expect(resumedPlayerTwo.locator(".result-card")).toContainText(
    "Player 1 23 · Player 2 2",
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
