import type { StoredServer } from "../storedServerContract";
import type { ConnectionState, Frame, Transport } from "../transport";
import { WORKER_SESSION_FEED_TYPES, WorkerSessionFeed } from "./workerSessionFeed";
import {
  initialWorkerState,
  serverWorkers,
  workerReducer,
  workerSessionFeedAction,
  type Worker,
  type WorkerConnectionState,
  type WorkerSessionsState,
} from "./workers";

const connectionOf = (state: ConnectionState): WorkerConnectionState =>
  state === "connected" ? "connected" : state === "connecting" ? "connecting" : "offline";

/**
 * The current computer's Sessions, kept by its sequenced feed: the daemon
 * sends the full list on every connect, then numbered changes. A lost change
 * (a gap in `seq`, or a heartbeat ahead) asks for the list again once. The
 * rows outlive a disconnect (the list says it is offline, not empty) and end
 * with the computer: `stop()` on a switch.
 */
export class WorkerSessions {
  private state: WorkerSessionsState = initialWorkerState;
  private readonly feed = new WorkerSessionFeed();
  private readonly stops: Array<() => void> = [];

  constructor(
    private readonly transport: Transport,
    private readonly server: StoredServer,
    private readonly changed: (sessions: WorkerSessions) => void,
  ) {
    const identity = { serverId: server.id, serverName: server.name, serverUrl: server.url };
    const onFrame = (frame: Frame) => {
      const verdict = this.feed.accept(frame);
      if (verdict === "resync") {
        try {
          transport.send({ type: "list_worker_sessions" });
        } catch {
          this.feed.resyncNotSent();
        }
        return;
      }
      if (verdict !== "apply") return;
      const action = workerSessionFeedAction(frame, identity);
      if (action) this.apply(action);
    };
    for (const type of WORKER_SESSION_FEED_TYPES) this.stops.push(transport.on(type, onFrame));
    this.stops.push(
      transport.on("connected", () => this.feed.reset()),
      transport.onState((state) => this.apply({ type: "SET_SERVER_CONNECTION_STATE", serverId: server.id, connectionState: connectionOf(state) })),
    );
    this.apply({ type: "SET_SERVER_CONNECTION_STATE", serverId: server.id, connectionState: connectionOf(transport.state) });
  }

  /** The computer's Sessions in the list's order (Brain's hidden host is never among them). */
  get workers(): Worker[] {
    return serverWorkers(this.state, this.server.id);
  }

  /** Whether this connection's list has arrived: before it, "no Sessions" is not known. */
  get hydrated(): boolean {
    return Boolean(this.state.hydratedServers[this.server.id]);
  }

  get connection(): WorkerConnectionState {
    return this.state.serverConnections[this.server.id] ?? "offline";
  }

  find(workerId: string): Worker | undefined {
    return this.workers.find((worker) => worker.id === workerId);
  }

  stop(): void {
    this.stops.splice(0).forEach((stop) => stop());
  }

  private apply(action: Parameters<typeof workerReducer>[1]): void {
    const next = workerReducer(this.state, action);
    if (next === this.state) return;
    this.state = next;
    this.changed(this);
  }
}
