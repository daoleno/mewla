import type { JSX } from "nsh";
import { signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import {
  baseName,
  growthText,
  holdKindLabel,
  holdOwnerTitle,
  restoreCollected,
  type HoldKind,
  type HoldsCollected,
  type ResourceHolds,
} from "../../../core/resources/holds";
import { formatBytes } from "../../../core/resources/resourceTelemetry";
import { DateStyle, formatDate } from "../../../core/timeline/dates";
import { Notice, Section, SectionLabel } from "../../kit/kit";
import { daemon } from "../daemon";

/** The restore in flight (its trash id) and the last one that failed. */
const restoring = signal<string | null>(null);
const restoreError = signal("");

/**
 * What Mewla holds on this computer (DESIGN C.11): each kind with its size, oldest item and 7-day growth;
 * the Sessions holding the most; what is kept and until when; what was collected today, with Restore.
 */
export function Holds(props: { holds: ResourceHolds; onRestored: () => void }) {
  const h = () => props.holds;
  const restore = (item: HoldsCollected) => {
    const client = daemon();
    if (!client || restoring()) return;
    restoring.set(item.id);
    restoreError.set("");
    restoreCollected(client, item.id)
      .then(() => props.onRestored())
      .catch((error: unknown) => restoreError.set(error instanceof Error ? error.message : "Restore failed."))
      .finally(() => restoring.set(null));
  };
  return (
    <View testID="holds" class="mt-2">
      <SectionLabel title="Held by Mewla" icon="layers" />
      <Section testID="holds-kinds" flush>
        <If when={h().kinds.length} else={<Row title="Not measured yet" />}>
          {h().kinds.map((k) => <Row testID={`hold-${k.kind}`} title={holdKindLabel(k.kind)} meta={kindMeta(k)} value={formatBytes(k.bytes)} />)}
        </If>
      </Section>
      <If when={h().owners.length}>
        <SectionLabel title="Most held by" icon="sessions" />
        <Section testID="holds-owners" flush>
          {h().owners.map((o) => <Row title={holdOwnerTitle(o)} meta={o.workTitle ? o.sessionId : undefined} value={formatBytes(o.bytes)} />)}
        </Section>
      </If>
      <If when={h().kept.length}>
        <SectionLabel title="Kept" icon="bookmark" count={h().kept.length} />
        <Section testID="holds-kept" flush>
          {h().kept.map((k) => <Row title={baseName(k.path)} meta={k.why} value={k.until ? `Until ${formatDate(new Date(k.until), DateStyle.shortDate)}` : undefined} />)}
        </Section>
      </If>
      <SectionLabel title="Collected today" icon="trash" count={`${h().today.items} · ${formatBytes(h().today.bytes)}`} testID="holds-today" />
      <If when={restoreError()}><Notice testID="holds-restore-failed" title="Restore failed" detail={restoreError()} /></If>
      <If when={h().today.restorable.length}>
        <Section testID="holds-restorable" flush>
          {h().today.restorable.map((item) => (
            <Row testID={`collected-${item.id}`} title={baseName(item.original)} meta={item.reason} value={formatBytes(item.bytes)}>
              <Button
                testID={`restore-${item.id}`}
                accessibilityLabel={`Restore ${baseName(item.original)}`}
                disabled={restoring() !== null}
                onPress={() => restore(item)}
                class="ml-2 h-8 flex-row items-center rounded-full bg-secondary px-3 active:bg-accent"
              >
                <Text class="text-sub font-semibold">{restoring() === item.id ? "Restoring" : "Restore"}</Text>
              </Button>
            </Row>
          ))}
        </Section>
      </If>
    </View>
  );
}

function kindMeta(k: HoldKind): string {
  const parts = [`${k.count} ${k.count === 1 ? "item" : "items"}`];
  if (k.oldest) parts.push(`oldest ${formatDate(new Date(k.oldest), DateStyle.shortDate)}`);
  const growth = growthText(k.growth7d, formatBytes);
  if (growth) parts.push(growth);
  return parts.join(" · ");
}

function Row(props: { title: string; meta?: string; value?: string; testID?: string; children?: JSX.Element }) {
  return (
    <View testID={props.testID}>
      <View class="flex-row items-center gap-3 px-4 py-3">
        <View class="min-w-0 flex-1">
          <Text class="text-body line-clamp-1">{props.title}</Text>
          <If when={props.meta}><Text class="text-sub text-muted-foreground line-clamp-2">{props.meta}</Text></If>
        </View>
        <If when={props.value}><Text class="font-mono text-code" style={{ fontVariant: ["tabular-nums"] }}>{props.value}</Text></If>
        {props.children ?? null}
      </View>
      <View class="absolute bottom-0 right-0 h-px bg-border" style={{ left: 16 }} />
    </View>
  );
}
