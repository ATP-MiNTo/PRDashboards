import { expect, test, type Page } from "@playwright/test";
import {
  APP,
  configure,
  fakeState,
  openSettings,
  resetFakes,
  signIn,
  syncUntil,
  useOnlyGitHubToken,
} from "./helpers";

// Several GitHub tokens (docs/features/github-tokens.md): one token for each owner, added in the
// Accounts dialog. The fake has two owners' tokens: github_pat_acme (octocat) writes in acme/* and
// only reads oss/lib, github_pat_oss (hubot) writes in oss/lib. Each repository is fetched and
// written with the token whose account has the highest role there.

type GitHubState = {
  merges: number;
  calls: { op: string; token: string; repo: string }[];
};

const FIRST = "Token (octocat)";

test.beforeEach(async ({ page, request }) => {
  await resetFakes(request);
  await configure(request, "github", { readOnly: true });
  await signIn(page, "tokens");
  // The first token comes from the Setup page, like for a person who connects GitHub for the first time.
  await useOnlyGitHubToken(page, "github_pat_acme");
});

const cardOf = (page: Page, title: string) =>
  page.locator("article[data-pr]").filter({ hasText: title });

async function openAccounts(page: Page) {
  const sidebar = await openSettings(page);
  await sidebar.getByRole("button", { name: "Manage accounts" }).click();
  const dialog = page.getByRole("dialog", { name: "Accounts" });
  await expect(dialog).toBeVisible();
  return dialog;
}

async function addOssToken(page: Page) {
  const dialog = await openAccounts(page);
  await expect(dialog.getByText("1 of 10")).toBeVisible();
  const form = dialog.getByRole("form", { name: "Add token" });
  await form.getByLabel("Token name (optional)").fill("OSS");
  await form.getByLabel("Personal access token").fill("github_pat_oss");
  await form.getByRole("button", { name: "Validate and add" }).click();
  await expect(dialog.getByRole("status")).toHaveText("Added OSS.");
  await expect(dialog.getByText("2 of 10")).toBeVisible();
  return dialog;
}

async function closeAccounts(page: Page) {
  await page
    .getByRole("dialog", { name: "Accounts" })
    .getByRole("button", { name: "Close" })
    .click();
  await expect(page.getByRole("dialog", { name: "Accounts" })).toBeHidden();
}

test("a second token takes the repository where it has the higher role", async ({
  page,
  request,
}) => {
  await page.goto(APP + "/");
  await syncUntil(page, "RO-41 Someone else's repository");
  const card = cardOf(page, "RO-41 Someone else's repository");
  const merge = card.getByRole("button", { name: "Merge", exact: true });
  // With the first token only, oss/lib is read-only and Merge says why, naming the token.
  await expect(merge).toHaveAttribute("aria-disabled", "true");
  await expect(merge).toHaveAccessibleDescription(
    `Read-only here: ${FIRST} can't write to oss/lib`,
  );

  const dialog = await addOssToken(page);
  const rows = dialog.getByRole("list", { name: "Connected tokens" });
  await expect(rows.getByRole("listitem")).toHaveCount(2);
  await closeAccounts(page);

  // The add wakes the poller: oss/lib moves to the new token and Merge is no longer blocked.
  await expect(merge).not.toHaveAttribute("aria-disabled", "true", {
    timeout: 15_000,
  });
  await expect(merge).toHaveAccessibleDescription("");

  // The VIA badge of each repository is the name of its token, with the hint beside it, so the
  // two tokens differ by their text and not only by colour.
  const sidebar = await openSettings(page);
  await sidebar.getByRole("button", { name: /^Repositories/ }).click();
  const repoRow = (name: string) =>
    sidebar.getByRole("listitem").filter({
      has: page.getByRole("button", { name: `Settings for ${name}` }),
    });
  await expect(repoRow("oss/lib")).toContainText("OSS");
  await expect(repoRow("acme/api")).toContainText(FIRST);
  await expect(repoRow("acme/api")).not.toContainText("OSS");
  await expect(
    repoRow("oss/lib").getByRole("button", {
      name: "About the token of oss/lib",
    }),
  ).toHaveAccessibleDescription(
    "Fetched with OSS (fine-grained token, hubot). Your role here: Write. A fine-grained token works for one owner only.",
  );
  await expect(
    repoRow("acme/api").getByRole("button", {
      name: "About the token of acme/api",
    }),
  ).toHaveAccessibleDescription(
    `Fetched with ${FIRST} (fine-grained token, octocat). Your role here: Write. A fine-grained token works for one owner only.`,
  );

  // The write goes out with the token of the repository, not the primary one.
  await merge.click();
  const panel = page.getByRole("dialog", { name: "Merge oss/lib #41?" });
  await panel
    .getByRole("checkbox", { name: "Merge without moving any ticket" })
    .check();
  await panel.getByRole("button", { name: "Merge without ticket" }).click();
  await expect(cardOf(page, "RO-41 Someone else's repository")).toHaveCount(0);
  const github = await fakeState<GitHubState>(request, "github");
  expect(github.merges).toBe(1);
  expect(
    github.calls.filter((c) => c.op === "merge").map((c) => [c.token, c.repo]),
  ).toEqual([["github_pat_oss", "oss/lib"]]);
  // Both tokens searched on every refresh.
  const searchers = new Set(
    github.calls.filter((c) => c.op === "search").map((c) => c.token),
  );
  expect(searchers).toEqual(new Set(["github_pat_acme", "github_pat_oss"]));
});

test("a token is renamed and removed with a confirmation, and its repository falls back to the other token", async ({
  page,
}) => {
  await page.goto(APP + "/");
  await syncUntil(page, "RO-41 Someone else's repository");
  const dialog = await addOssToken(page);

  // Rename the first token.
  await dialog.getByRole("button", { name: `Rename ${FIRST}` }).click();
  await dialog.getByLabel(`Name of ${FIRST}`).fill("Work");
  await dialog.getByRole("button", { name: "Save name" }).click();
  await expect(
    dialog.getByRole("button", { name: "Rename Work" }),
  ).toBeVisible();
  await expect(dialog.getByText(FIRST)).toHaveCount(0);

  // A name that another token has is refused in the row.
  await dialog.getByRole("button", { name: "Rename OSS" }).click();
  await dialog.getByLabel("Name of OSS").fill("work");
  await dialog.getByRole("button", { name: "Save name" }).click();
  await expect(dialog.getByRole("alert")).toContainText(/already/i);
  await dialog.getByRole("button", { name: "Cancel" }).click();

  // Removing asks first, and Keep keeps the token.
  await dialog.getByRole("button", { name: "Remove OSS" }).click();
  const confirm = dialog.getByRole("group", { name: "Remove OSS?" });
  await expect(confirm).toContainText(
    "Repositories only it can see leave the list.",
  );
  await confirm.getByRole("button", { name: "Keep" }).click();
  await expect(dialog.getByText("2 of 10")).toBeVisible();
  await dialog.getByRole("button", { name: "Remove OSS" }).click();
  await dialog
    .getByRole("group", { name: "Remove OSS?" })
    .getByRole("button", { name: "Confirm remove" })
    .click();
  await expect(dialog.getByText("1 of 10")).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Rename OSS" })).toHaveCount(
    0,
  );
  await closeAccounts(page);

  // oss/lib is only read with the remaining token again: Merge is blocked and names it.
  const merge = cardOf(page, "RO-41 Someone else's repository").getByRole(
    "button",
    { name: "Merge", exact: true },
  );
  await expect(merge).toHaveAccessibleDescription(
    "Read-only here: Work can't write to oss/lib",
    { timeout: 15_000 },
  );
});
