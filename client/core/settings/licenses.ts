/** Open-source licenses shown in Settings; the data is ui/more/settings/licenses/licenses.json. */

export interface LicenseItem {
  name: string;
  version?: string;
  license: string;
  url?: string;
  platforms?: string[];
  note?: string;
  /** Key into LicenseData.texts. */
  text?: string;
  /** Packages compiled into this item. */
  includes?: LicenseItem[];
}

export interface LicenseSection {
  title: string;
  items: LicenseItem[];
}

export interface LicenseData {
  sections: LicenseSection[];
  texts: Record<string, string>;
}

/** Id of an item for the detail page: "<section>/<item>[/<included item>]". */
export function licenseItemId(...indices: number[]): string {
  return indices.join("/");
}

export function findLicenseItem(data: LicenseData, id: string | undefined): LicenseItem | null {
  if (!/^\d+\/\d+(\/\d+)?$/.test(id ?? "")) return null;
  const [section, index, included] = id!.split("/").map(Number);
  const item = data.sections[section]?.items[index] ?? null;
  if (included === undefined) return item;
  return item?.includes?.[included] ?? null;
}

const PLATFORM_NAMES: Record<string, string> = { android: "Android", ios: "iOS", web: "Web" };

/** "1.2.3 · MIT · Android only" */
export function licenseSummary(item: LicenseItem): string {
  const parts = [item.version, item.license];
  if (item.platforms?.length === 1) parts.push(`${PLATFORM_NAMES[item.platforms[0]] ?? item.platforms[0]} only`);
  const included = item.includes?.length ?? 0;
  if (included > 0) parts.push(`includes ${included} ${included === 1 ? "package" : "packages"}`);
  return parts.filter(Boolean).join(" · ");
}
