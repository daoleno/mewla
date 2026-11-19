// The app on a scripted computer (FakeDaemonApp) whose agents' viewer sockets are played by the test
// (fakeViewerSockets.ts): for `nsh test` only.
import "../../FakeDaemonApp";
import { installViewerSockets } from "../../more/screen/testing/fakeViewerSockets";

installViewerSockets((url) => /\/(display|screen)\/viewer\b/.test(url));
