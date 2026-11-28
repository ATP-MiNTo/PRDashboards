import { expect, test, type Page } from "@playwright/test";
import {
  APP,
  openSettings,
  resetFakes,
  signIn,
  disconnectAll,
} from "./helpers";

// The sidebar with a real account's worth of repositories, and its 30 second auto-collapse.

test.beforeEach(async ({ page, request }) => {
  await resetFakes(request);
  await signIn(page);
  await disconnectAll(page);
  await page.goto(APP + "/setup/");
  await page.getByLabel("Personal access token").fill("ghp_classic_ok");
  await page.getByRole("button", { name: "Validate and save" }).click();
  await expect(page.getByText("Connected as octocat")).toBeVisible();
});

const LONG = [
  "platform-engineering-internal-developer-experience-tooling",
  "customer-facing-billing-and-invoicing-microservice-v2",
  "a",
  "data-warehouse-etl-pipelines-and-orchestration-jobs",
];

// 25 repositories with very long names, in place of the few the fake GitHub has.
async function manyRepos(page: Page) {
  const repos = Array.from({ length: 25 }, (_, i) => ({
    approvalsRequired: 1,
    branchRules: [],
    defaultMethod: "squash",
    deleteBranch: "default",
    id: `R_${i}`,
    inheritMethod: i % 2 === 0,
    isNew: false,
    nameWithOwner: `some-very-long-organization-name/${LONG[i % LONG.length]}-${i}`,
    openPrs: i % 4,
  }));
  await page.route("**/api/repos", (route) =>
    route.request().method() === "GET"
      ? route.fulfill({ json: { repos } })
      : route.fallback(),
  );
}

for (const width of [1440, 1100]) {
  test(`nothing in the sidebar is cut off with 25 long repository names at ${width}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await manyRepos(page);
    await page.goto(APP + "/");
    const sidebar = await openSettings(page);
    await sidebar
      .getByRole("button", { name: /^Repositories/ })
      .click();
    await expect(
      sidebar.getByRole("list", { name: "Repositories" }).getByRole("listitem"),
    ).toHaveCount(25);

    const body = sidebar.locator('div[class*="sidebarBody"]');
    // Nothing scrolls sideways: the content is as wide as the box that clips it.
    const [client, scroll] = await body.evaluate((el) => [
      el.clientWidth,
      el.scrollWidth,
    ]);
    expect(scroll).toBeLessThanOrEqual(client);

    const box = (await body.boundingBox())!;
    const inside = async (locator: ReturnType<Page["locator"]>) => {
      const b = (await locator.boundingBox())!;
      expect(b.x).toBeGreaterThanOrEqual(box.x - 0.5);
      expect(b.x + b.width).toBeLessThanOrEqual(box.x + box.width + 0.5);
    };
    await inside(sidebar.getByLabel("Default merge method"));
    await inside(sidebar.getByRole("button", { name: "Select all" }));
    await inside(sidebar.getByRole("button", { name: "Clear" }));
    // Every row keeps its VIA badge and its gear, and the gear opens the repository's settings.
    const gears = sidebar.getByRole("button", { name: /^Settings for / });
    await expect(gears).toHaveCount(25);
    for (const gear of await gears.all()) await inside(gear);
    for (const via of await sidebar.getByText("Token", { exact: true }).all()) {
      await inside(via);
    }
    await testInfo.attach(`sidebar-${width}`, {
      body: await page.screenshot(),
      contentType: "image/png",
    });
    await gears.last().click();
    await expect(
      page.getByRole("dialog", { name: /some-very-long/ }),
    ).toBeVisible();
  });
}

test("the list page does not scroll sideways at 390px, with a tooltip open or not", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(APP + "/");
  await expect(page.locator("article[data-pr]").first()).toBeVisible();
  const fits = () =>
    page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    );
  expect(await fits()).toBe(true);
  // A tooltip near the edge stays inside the page while it is shown, too.
  const tipped = page.locator("[data-tip]").first();
  await tipped.scrollIntoViewIfNeeded();
  await tipped.hover();
  expect(await fits()).toBe(true);
});

test.describe("auto-collapse", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.clock.install();
    await page.goto(APP + "/");
    await openSettings(page);
  });

  const expanded = (page: Page) =>
    page
      .getByRole("button", { name: "Settings sidebar" })
      .getAttribute("aria-expanded");

  test("does not fold while a button in it has the focus, and folds 30 seconds after the focus left", async ({
    page,
  }) => {
    const sidebar = page.getByRole("complementary", { name: "Settings" });
    await sidebar
      .getByRole("button", { name: /^Repositories/ })
      .click();
    await expect(
      sidebar.getByRole("button", { name: /^Repositories/ }),
    ).toBeFocused();
    await page.mouse.move(100, 300);
    await page.clock.runFor(90_000);
    expect(await expanded(page)).toBe("true");
    await page.mouse.move(800, 400);
    await page.clock.runFor(120_000);
    expect(await expanded(page)).toBe("true");
    await sidebar.getByRole("button", { name: /^Repositories/ }).blur();
    await page.clock.runFor(29_000);
    expect(await expanded(page)).toBe("true");
    await page.clock.runFor(2_000);
    expect(await expanded(page)).toBe("false");
  });

  test("stays open while a dialog is open over the page", async ({ page }) => {
    const sidebar = page.getByRole("complementary", { name: "Settings" });
    await sidebar.getByRole("button", { name: "Status sync rules" }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.mouse.move(800, 400);
    await page.clock.runFor(120_000);
    expect(await expanded(page)).toBe("true");
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toBeHidden();
    // The dialog gave the focus back to the button in the sidebar, which holds it open.
    await page.mouse.move(850, 420);
    await page.clock.runFor(120_000);
    expect(await expanded(page)).toBe("true");
    await sidebar.getByRole("button", { name: "Status sync rules" }).blur();
    await page.clock.runFor(31_000);
    expect(await expanded(page)).toBe("false");
  });
});
