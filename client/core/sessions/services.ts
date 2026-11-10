/** From the Expo app's services/sessionServices.ts. */
export type SessionServiceURL = {
  label: string;
  url: string;
  address: string;
  kind: "lan" | "tailscale" | string;
};

export type SessionServiceInterface = {
  name: string;
  address: string;
  kind: "lan" | "tailscale" | string;
};

export type ServiceTunnel = { status: "starting" | "running" | "stopping" | "stopped" | "failed"; url?: string; error?: string; generation: string };

export type SessionService = {
  generation?: string;
  tunnel?: ServiceTunnel;
  id: string;
  worker_id: string;
  worker_name: string;
  project?: string;
  cwd?: string;
  command?: string;
  process?: string;
  pid: number;
  port: number;
  protocol: string;
  binds: string[];
  urls: SessionServiceURL[];
  local_only: boolean;
  source?: "session" | "persistent" | string;
  unit?: string;
  state?: "active" | "inactive" | "error" | string;
  status_detail?: string;
};

export type SessionServiceSnapshot = {
  generated_at?: string;
  interfaces: SessionServiceInterface[];
  services: SessionService[];
};


type Requester = {
  request<T>(type: string, body: Record<string, unknown>, options: { reply: string; timeoutMs: number }): Promise<T>;
};

const text = (value: unknown) => (typeof value === "string" ? value : undefined);

/** One row of `session_service_list`, with every field the sheet reads made safe. */
export function normalizeSessionService(value: unknown): SessionService {
  const service = (value && typeof value === "object" ? value : {}) as Record<string, unknown>;
  return {
    ...(service as Partial<SessionService>),
    id: text(service.id) ?? "",
    worker_id: text(service.worker_id) ?? "",
    worker_name: text(service.worker_name) ?? "",
    project: text(service.project),
    cwd: text(service.cwd),
    command: text(service.command),
    process: text(service.process),
    pid: typeof service.pid === "number" ? service.pid : 0,
    port: typeof service.port === "number" ? service.port : 0,
    protocol: text(service.protocol) ?? "tcp",
    binds: Array.isArray(service.binds) ? (service.binds as string[]) : [],
    urls: Array.isArray(service.urls) ? (service.urls as SessionServiceURL[]) : [],
    local_only: Boolean(service.local_only),
    source: text(service.source),
    unit: text(service.unit),
    state: text(service.state),
    status_detail: text(service.status_detail),
  };
}

/** The computer's listening ports (`list_session_services`). */
export async function listSessionServices(daemon: Requester): Promise<SessionServiceSnapshot> {
  const reply = await daemon.request<Record<string, unknown>>("list_session_services", {}, { reply: "session_service_list", timeoutMs: 10_000 });
  return {
    generated_at: text(reply.generated_at),
    interfaces: Array.isArray(reply.interfaces) ? (reply.interfaces as SessionServiceInterface[]) : [],
    services: Array.isArray(reply.services) ? reply.services.map(normalizeSessionService) : [],
  };
}

/** Starts, stops or reads a service's Quick Tunnel; the answer must be for this generation of the service. */
export async function serviceTunnel(daemon: Requester, serviceId: string, generation: string, action: "start" | "stop" | "status"): Promise<ServiceTunnel> {
  const reply = await daemon.request<Record<string, unknown>>(
    "service_tunnel",
    { service_id: serviceId, service_generation: generation, tunnel_action: action },
    { reply: "service_tunnel", timeoutMs: 10_000 },
  );
  const tunnel = reply.tunnel as ServiceTunnel | undefined;
  if (!tunnel || tunnel.generation !== generation) throw new Error("Service changed. Refresh Services.");
  return tunnel;
}
