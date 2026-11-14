import { openUrl, Text, View } from "nsh";
import { findLicenseItem, licenseItemId, licenseSummary, type LicenseData, type LicenseItem } from "../../../core/settings/licenses";
import { EmptyState, Page, Row, Section } from "../../kit/kit";
import { tile } from "../../kit/RowTile";
import type { PageNav } from "../routes";
import data from "./licenses/licenses.json";

const licenses = data as LicenseData;

/** Settings → Open-source Licenses: what the app ships, by section. */
export function Licenses(props: { nav: PageNav }) {
  return (
    <Page display title="Open-source Licenses" testID="licenses-page">
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
  if (!item) return <Page display title="License"><EmptyState icon="book" title="License not found" /></Page>;
  const text = item.text ? licenses.texts[item.text] : "";
  return (
    <Page display title={item.name} testID="license-page">
      <Section>
        <Row title={item.name} subtitle={licenseSummary(item)} />
        <If when={item.url}>{(url) => <Row lead={tile("open-external")} title="Source" value={url().replace(/^https?:\/\//, "")} onPress={() => void openUrl(url())} />}</If>
      </Section>
      <If when={item.note}>{(note) => <View class="mt-3 px-4"><Text class="text-sm text-muted-foreground">{note()}</Text></View>}</If>
      <If when={text}>{(text) => <View class="mt-4 rounded-2xl border border-border bg-card px-4 py-3" selectable>
          <Text testID="license-text" class="font-mono text-xs leading-5 select-text">{text()}</Text>
        </View>}</If>
      <If when={item.includes?.length}><Section title={`Includes ${item.includes!.length} ${item.includes!.length === 1 ? "package" : "packages"}`}>
          {item.includes!.map((included, n) => <Row title={included.name} subtitle={licenseSummary(included)} chevron onPress={() => props.nav.push({ name: "settings-license", id: `${props.id.split("/").slice(0, 2).join("/")}/${n}` })} />)}
        </Section></If>
    </Page>
  );
}
