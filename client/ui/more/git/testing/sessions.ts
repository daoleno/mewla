import { host } from "nsh";
import type { Worker } from "../../../../core/sessions/workers";
import { workers } from "../../../session";

/**
 * The current computer's Sessions as `host.params.workers` lists them, before the More
 * harness mounts: the Session list arrives over a transport the page tests do not have.
 */
const listed = (host.params as { workers?: Array<Partial<Worker> & { id: string }> }).workers ?? [];
workers.set(listed.map((worker) => ({ name: worker.id, status: "running", ...worker }) as Worker));
