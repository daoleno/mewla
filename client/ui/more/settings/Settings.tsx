import { current, servers, switchTo } from "../../session";
import { Page, Row, Section } from "../kit/kit";
import type { PageNav } from "../routes";

/** Settings: the computers first, then each thing this phone decides. */
export function Settings(props: { nav: PageNav; onPair: () => void }) {
  return (
    <Page title="Settings" testID="settings">
      <Section title="Computers">
        {servers().map((s) => (
          <Row title={s.name || s.url} value={s.id === current()?.id ? "Current" : undefined} onPress={() => switchTo(s)} />
        ))}
        <Row testID="pair-another" icon="add" title="Pair a computer" onPress={props.onPair} />
      </Section>
    </Page>
  );
}
