// The public Mac walk starts at the same install button a new visitor uses.
import { join } from "node:path";

const { chromium } = await import(process.env.PLAYWRIGHT_CORE);
const [expectedURL, destination, evidence] = process.argv.slice(2);
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  await page.goto("https://mewla.ai/", { waitUntil: "domcontentloaded" });
  const install = page.locator("#get");
  await install.getByRole("tab", { name: "Mac app" }).click();
  const button = install.getByRole("link", { name: "Download for Mac" });
  const href = await button.getAttribute("href");
  if (href !== expectedURL) throw new Error("The live Mac install button has an unexpected destination");
  await button.scrollIntoViewIfNeeded();
  await page.screenshot({ path: join(evidence, "00-live-install.png") });
  const downloading = page.waitForEvent("download", { timeout: 120000 });
  await button.click();
  const download = await downloading;
  if (download.suggestedFilename() !== "Mewla-macos-arm64.dmg") throw new Error("Unexpected download name");
  await download.saveAs(destination);
  console.log("Downloaded from the live Mac install button: " + href);
} finally {
  await browser.close();
}
