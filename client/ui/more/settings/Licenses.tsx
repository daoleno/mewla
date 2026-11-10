import { openUrl, Text, View } from "nsh";
import { findLicenseItem, licenseItemId, licenseSummary, type LicenseData, type LicenseItem } from "../../../core/settings/licenses";
import { EmptyState, Page, Row, Section } from "../kit/kit";
import type { PageNav } from "../routes";
import data from "./licenses/licenses.json";

const licenses = data as LicenseData;

/** Settings → Open-source Licenses: what the app ships, by section. */
export function Licenses(props: { nav: PageNav }) {
  return (
    <Page title="Open-source Licenses" testID="licenses-page">
      {licenses.sections.map((section, s) => (
        <Section title={section.title}>
          {section.items.map((item, i) => <Row testID={`license-${s}-${i}`} title={item.name} subtitle={licenseSummary(item)} chevron onPress={() => props.nav.push({ name: "settings-license", id: licenseItemId(s, i) })} />)}
        </Section>
      ))}
    </Page>
  );
}

/** One work: where its source is, any note, and its license text. */
export function License(props: { id: string; nav: PageNav }) {
  const item: LicenseItem | null = findLicenseItem(licenses, props.id);
  if (!item) return <Page title="License"><EmptyState icon="book" title="License not found" /></Page>;
  const text = item.text ? licenses.texts[item.text] : "";
  return (
    <Page title={item.name} testID="license-page">
      <Section>
        <Row title={item.name} subtitle={licenseSummary(item)} />
        {item.url ? <Row icon="open-external" title="Source" value={item.url.replace(/^https?:\/\//, "")} onPress={() => void openUrl(item.url!)} /> : null}
      </Section>
      {item.note ? <Text class="mt-3 px-4 text-sm text-muted-foreground">{item.note}</Text> : null}
      {text ? (
        <View class="mt-4 rounded-2xl border border-border bg-card px-4 py-3">
          <Text testID="license-text" class="font-mono text-xs leading-5">{text}</Text>
        </View>
      ) : null}
      {item.includes?.length ? (
        <Section title={`Includes ${item.includes.length} ${item.includes.length === 1 ? "package" : "packages"}`}>
          {item.includes.map((included, n) => <Row title={included.name} subtitle={licenseSummary(included)} chevron onPress={() => props.nav.push({ name: "settings-license", id: `${props.id.split("/").slice(0, 2).join("/")}/${n}` })} />)}
        </Section>
      ) : null}
    </Page>
  );
}
