import { host } from "nsh";
import type { Worker } from "../../../../../core/sessions/workers";
import { workers } from "../../../../session";

// The current computer's Sessions, as a test gives them (`host.params.workers`,
// later `app.host.send("app.workers", list)`), before the page opens.
workers.set(((host.params as unknown as { workers?: Worker[] }).workers ?? []) as Worker[]);
host.on("app.workers", (list) => workers.set(list as unknown as Worker[]));
