import { cleanup, effect, lifecycle, signal, untracked, View } from "nsh";
import { providerUsage } from "../../../core/providers/requests";
import {
  errorLine,
  requestCostLabel,
  requestLine,
  usageCostLabel,
  USAGE_DAYS,
  usageSince,
  usageTotals,
  localDay,
  type ProviderUsage as Usage,
} from "../../../core/providers/usage";
import { formatTokens } from "../../../core/stats/format";
import { AgentIcon } from "../../icons/AgentIcon";
import { Caption, Row, Section, ShowMore, StatGrid, StatTile } from "../../kit/kit";
import { LEAD_SLOT, tile } from "../../kit/RowTile";
import { connected, daemon } from "../daemon";

/** Rows shown before "Show more". */
const FIRST_ROWS = 5;
/** How often an open Provider page reads its usage again. */
const REREAD_MS = 10_000;

/**
 * What one Provider was asked, as its daemon kept it: the last 30 days in tiles, then its recent requests (tokens,
 * cache, cost when the model's price is known) and its recent failures.
 */
export function ProviderUsage(props: { connectionId: string }) {
  const usage = signal<Usage | null>(null);
  const now = signal(Date.now());
  const allRequests = signal(false);
  const allErrors = signal(false);

  async function read(): Promise<void> {
    const current = untracked(daemon);
    if (!current) return;
    const next = await providerUsage(current, props.connectionId);
    now.set(Date.now());
    if (next) usage.set(next);
  }
  effect(() => {
    if (!connected() || lifecycle() !== "active") return;
    untracked(() => void read());
    const timer = setInterval(() => void read(), REREAD_MS);
    cleanup(() => clearInterval(timer));
  });

  const month = () => usageTotals(usage()!, usageSince(now(), USAGE_DAYS));
  const today = () => usageTotals(usage()!, localDay(now()));
  const requests = () => (allRequests() ? usage()!.requests : usage()!.requests.slice(0, FIRST_ROWS));
  const errors = () => (allErrors() ? usage()!.errors : usage()!.errors.slice(0, FIRST_ROWS));

  return (
    <If when={usage()}>
      <View testID="provider-usage">
        {/* Tiles stand on the page, as Stats' do, under a group's title. */}
        <View class="mt-6">
          <View class="min-h-[18px] flex-row items-center px-4"><Caption role="header" inRow text={`Usage · ${USAGE_DAYS} days`} /></View>
          <StatGrid testID="provider-usage-totals">
            <StatTile label="Cost" valueTestID="provider-usage-cost" value={usageCostLabel(month())} muted={!month().costKnown && month().cost === 0} detail={`${usageCostLabel(today())} today`} />
            <StatTile label="Tokens" valueTestID="provider-usage-tokens" value={formatTokens(month().tokens)} detail={`${formatTokens(today().tokens)} today`} />
            <StatTile
              label="Requests"
              valueTestID="provider-usage-requests"
              value={String(month().requests)}
              status={month().errors > 0 ? { word: `${month().errors} failed`, tone: "warn" } : undefined}
              detail={`${today().requests} today`}
            />
          </StatGrid>
        </View>
        <If when={usage()!.requests.length > 0}><Section title="Recent requests" testID="provider-usage-recent">
          <>
            {requests().map((r, i) => (
              <Row
                testID={`provider-usage-request-${i}`}
                lead={{ kind: "logo", node: <AgentIcon kind={r.agent === "codex" ? "codex" : "claude"} size={28} slot={LEAD_SLOT} /> }}
                title={r.model || "Unknown model"}
                subtitle={requestLine(r, now())}
                subtitleOneLine
                value={requestCostLabel(r) || undefined}
              />
            ))}
            <If when={usage()!.requests.length > FIRST_ROWS}><ShowMore testID="provider-usage-more" hidden={usage()!.requests.length - FIRST_ROWS} expanded={allRequests()} onToggle={() => allRequests.set(!allRequests())} /></If>
          </>
        </Section></If>
        <If when={usage()!.errors.length > 0}><Section title="Recent errors" testID="provider-usage-errors">
          <>
            {errors().map((e, i) => (
              <Row testID={`provider-usage-error-${i}`} lead={tile("warning")} title={e.message} subtitle={errorLine(e, now())} subtitleOneLine />
            ))}
            <If when={usage()!.errors.length > FIRST_ROWS}><ShowMore testID="provider-usage-errors-more" hidden={usage()!.errors.length - FIRST_ROWS} expanded={allErrors()} onToggle={() => allErrors.set(!allErrors())} /></If>
          </>
        </Section></If>
      </View>
    </If>
  );
}
