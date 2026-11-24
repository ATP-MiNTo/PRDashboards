import { expect, test } from "@playwright/test";
import {
  APP,
  configure,
  disconnectAll,
  resetFakes,
  signIn,
} from "./helpers";

// TECH_SPEC section 12, flow 3: an organization that enforces SSO. The fake GitHub leaves out the
// pull requests of acme-corp and answers with the SAML header, as GitHub does. The banner links to
// the organization's SSO page and the other rows load.

test.beforeEach(async ({ page, request }) => {
  await resetFakes(request);
  await signIn(page);
  await disconnectAll(page);
});

test("an SSO-blocked organization shows the banner while the other rows load", async ({
  page,
  request,
}) => {
  await page.goto(APP + "/setup/");
  await page.getByLabel("Personal access token").fill("ghp_classic_ok");
  await page.getByRole("button", { name: "Validate and save" }).click();
  await expect(page.getByText("Connected as octocat")).toBeVisible();

  await page.goto(APP + "/");
  await expect(page.getByText("INP-104 Invoice export")).toBeVisible();
  await expect(page.getByText("has not authorized")).toHaveCount(0);

  await configure(request, "github", { sso: true });
  await page.getByRole("button", { name: "Sync", exact: true }).click();

  const banner = page.getByRole("alert").filter({ hasText: "acme-corp" });
  await expect(banner).toContainText("acme-corp has not authorized this token");
  await expect(banner.getByRole("link", { name: "Authorize" })).toHaveAttribute(
    "href",
    "https://github.com/orgs/acme-corp/sso",
  );
  await expect(page.getByText("INP-104 Invoice export")).toBeHidden();
  await expect(page.getByText("INP-101 Add login")).toBeVisible();
  await expect(page.getByText("INP-102 Single sign-on")).toBeVisible();
  await expect(page.getByText("Merge after #101")).toBeVisible();
});
