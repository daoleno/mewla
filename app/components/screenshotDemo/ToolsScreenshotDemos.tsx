import React, { useEffect, useMemo, useState } from "react";
import { useLocalSearchParams, useRouter } from "expo-router";
import { ResourcesView } from "../resources/ResourcesView";
import { SkillsPresentation } from "../skills/SkillsPresentation";
import { groupLogicalSkills } from "../../services/skillsScreenModel";
import { groupLogicalPlugins } from "../../services/pluginsScreenModel";
import { skillsOutsidePlugins } from "../../services/skillsPluginOwnership";
import type { PackageDetail, SkillsRequestState } from "../../services/skillsManagement";
import type { SkillsSurfaceSection } from "../../services/skillsSurfaceModel";
import {
  SCREENSHOT_PLUGIN_INVENTORY,
  SCREENSHOT_SKILLS_INVENTORY,
  SCREENSHOT_WORK_SERVER_ID,
  screenshotResourceTelemetry,
  screenshotWorkItems,
} from "../../services/screenshotToolsFixtures";
import { useWorkDispatch } from "../../store/work";

const NOOP = () => undefined;

function fixtureParam(value: string | string[] | undefined) {
  return Array.isArray(value) ? value[0] : value;
}

/**
 * Skills and Agent Plugins with fixture inventories. `fixture=plugins` opens
 * the Agent Plugins tab, `fixture=inspect` the mobile-qa inspector, and
 * `fixture=notice` the failure toast.
 */
export function SkillsScreenshotDemo({ header }: { header: React.ReactNode }) {
  const fixture = fixtureParam(useLocalSearchParams<{ fixture?: string | string[] }>().fixture);
  const [section, setSection] = useState<SkillsSurfaceSection>(fixture === "plugins" ? "plugins" : "skills");
  const [inspected, setInspected] = useState(fixture === "inspect" ? "mobile-qa" : null);
  const logicalPlugins = useMemo(() => groupLogicalPlugins(SCREENSHOT_PLUGIN_INVENTORY.installed), []);
  const standalone = useMemo(
    () => skillsOutsidePlugins(SCREENSHOT_SKILLS_INVENTORY.skills, logicalPlugins),
    [logicalPlugins],
  );
  const logicalSkills = useMemo(() => groupLogicalSkills(standalone), [standalone]);
  const copy = SCREENSHOT_SKILLS_INVENTORY.skills.find((skill) => skill.name === inspected);
  const detail: PackageDetail | undefined = copy
    ? {
        copyId: copy.id,
        skillName: copy.name,
        description: copy.description,
        enabled: copy.enabled,
        rootPath: copy.rootPath,
        canonicalPath: copy.canonicalPath,
        allowedRoot: copy.allowedRoot,
        location: copy.location,
        scope: copy.scope,
        agents: copy.agents,
        capability: copy.capability,
        files: [
          { path: "SKILL.md", size: 2140, mode: "0644", kind: "markdown", mediaType: "text/markdown", previewStatus: "ready" },
          { path: "scripts/run-qa.sh", size: 812, mode: "0755", kind: "text", mediaType: "text/x-shellscript", previewStatus: "ready" },
        ],
        preview: {
          path: "SKILL.md",
          kind: "markdown",
          mediaType: "text/markdown",
          status: "ready",
          size: 2140,
          bytesReturned: 2140,
          content: "---\nname: mobile-qa\n---\n\n# Mobile QA\n\nDrive the app on a phone and file what breaks.\n",
        },
      }
    : undefined;
  const inspectState: SkillsRequestState<PackageDetail> = detail
    ? { status: "ready", generation: 1, data: detail }
    : { status: "idle", generation: 0 };
  return (
    <>
      {header}
      <SkillsPresentation
        section={section}
        inventoryState={{ status: "ready", generation: 1, data: SCREENSHOT_SKILLS_INVENTORY }}
        logicalSkills={logicalSkills}
        pluginsState={{ status: "ready", generation: 1, data: SCREENSHOT_PLUGIN_INVENTORY }}
        logicalPlugins={logicalPlugins}
        skills={SCREENSHOT_SKILLS_INVENTORY.skills}
        mutationOperations={SCREENSHOT_SKILLS_INVENTORY.mutationOperations}
        preparingMutation=""
        mutationNotice={fixture === "notice" ? { kind: "error", message: "Could not delete changelog: the folder is read-only." } : null}
        currentServerAvailable
        serverName="Studio Mac"
        connection="connected"
        projectCwd=""
        pluginOwnedSkillCount={SCREENSHOT_SKILLS_INVENTORY.skills.length - standalone.length}
        inspectedName={inspected}
        inspectedCopyId={copy?.id ?? null}
        inspectState={inspectState}
        onSelectSection={setSection}
        onOpenSettings={NOOP}
        onRefreshSkills={NOOP}
        onRetryPlugins={NOOP}
        onInspectSkill={(skill) => setInspected(skill.name)}
        onInspectSkillCopy={async () => detail!}
        onDismissInspector={() => setInspected(null)}
        onDeleteSkill={NOOP}
        onUninstallPlugin={NOOP}
        onDismissNotice={NOOP}
        onViewSkillPlugin={() => setSection("plugins")}
        focusedPluginKey={null}
        onFocusPluginConsumed={NOOP}
      />
    </>
  );
}

/** Resources with a fixture telemetry; `fixture=loading|offline|error` for the quiet states. */
export function ResourcesScreenshotDemo({ header }: { header: React.ReactNode }) {
  const fixture = fixtureParam(useLocalSearchParams<{ fixture?: string | string[] }>().fixture);
  const telemetry = useMemo(() => screenshotResourceTelemetry(), []);
  return (
    <>
      {header}
      <ResourcesView
        telemetry={fixture ? null : telemetry}
        loading={fixture === "loading"}
        error={fixture === "error" ? "Resource telemetry timed out." : null}
        connected={fixture !== "offline"}
        hasServer
        serverName="Studio Mac"
        onOpenSettings={NOOP}
        onRetry={NOOP}
        now={telemetry.sampledAt}
      />
    </>
  );
}

/**
 * Seeds the Work store and opens the real Work route. Needs the seeded
 * current server `demo-server`; `fixture` picks the item (running by default).
 */
export function WorkScreenshotDemo() {
  const fixture = fixtureParam(useLocalSearchParams<{ fixture?: string | string[] }>().fixture);
  const dispatch = useWorkDispatch();
  const router = useRouter();
  useEffect(() => {
    dispatch({
      type: "WORK_ITEMS_SNAPSHOT",
      serverId: SCREENSHOT_WORK_SERVER_ID,
      serverName: "Studio Mac",
      serverUrl: "https://demo.invalid",
      workItems: screenshotWorkItems(),
    });
    router.replace({
      pathname: "/work/[id]",
      params: { id: `w-${fixture ?? "running"}`, serverId: SCREENSHOT_WORK_SERVER_ID },
    });
  }, [dispatch, fixture, router]);
  return null;
}
