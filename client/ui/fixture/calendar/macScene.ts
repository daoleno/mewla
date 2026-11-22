// Mewla Dev's Calendar scene for the captures beside the Calendar mocks (macos-fidelity.yml): `calendarScene` in the
// dev script, "busy" (10:44), "night" (the busy day at 22:00) or "quiet". Imported by MacFixtureApp before the app,
// so the clock Calendar reads when it loads is already the scene's.
import { host } from "nsh";
import { useCalendarScene, type CalendarScene } from "../calendarScene";

const SCENES: Record<string, CalendarScene> = { busy: { kind: "busy", at: "10:44" }, night: { kind: "busy", at: "22:00" }, quiet: { kind: "quiet", at: "10:44" } };
const name = (host.params as { devScript?: { calendarScene?: string } } | undefined)?.devScript?.calendarScene;
if (name && SCENES[name]) useCalendarScene(SCENES[name]!);
