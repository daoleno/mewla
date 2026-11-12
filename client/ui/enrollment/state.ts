import { computed, effect, lifecycle, signal, untracked } from "nsh";
import { decideEnrollment, decodePendingEnrollment, pendingEnrollments, type PendingEnrollment } from "../../core/enrollment";
import { current, onTransport } from "../session";
import { forgetOwnEnrollment, ownEnrollmentRequest } from "./ownRequest";

/** The current computer's requests from new devices, newest last; cleared on a switch or a disconnect. */
const requests = signal<PendingEnrollment[]>([]);

function refresh(): void {
  const server = untracked(current);
  if (!server) return;
  void pendingEnrollments(server)
    .then((pending) => untracked(current)?.id === server.id && requests.set(pending))
    .catch(() => {});
}

onTransport((transport) => {
  requests.set([]);
  const add = (request: PendingEnrollment) => requests.set([...untracked(requests).filter((r) => r.id !== request.id), request]);
  const stops = [
    transport.on("enrollment_request", (frame) => {
      const request = decodePendingEnrollment(frame as Record<string, unknown>);
      if (request) add(request);
    }),
    transport.on("enrollment_decision", (frame) => requests.set(untracked(requests).filter((r) => r.id !== frame.request_id))),
    transport.on("connected", refresh),
    transport.on("disconnected", () => requests.set([])),
  ];
  // Expired requests leave on their own.
  const timer = setInterval(() => {
    const live = untracked(requests).filter((r) => Date.parse(r.expiresAt) > Date.now());
    if (live.length !== untracked(requests).length) requests.set(live);
  }, 1000);
  return () => {
    stops.forEach((stop) => stop());
    clearInterval(timer);
    requests.set([]);
  };
});

// This browser's own request, left pending when a pairing link paired it instead: it is
// never shown, and once this browser is paired it is withdrawn, so no device is asked about it.
const withdrawn = new Set<string>();
effect(() => {
  const own = ownEnrollmentRequest();
  const server = current();
  const mine = own ? requests().find((r) => r.id === own) : undefined;
  if (!server || !mine) return;
  untracked(() => {
    withdrawn.add(mine.id);
    requests.set(requests().filter((r) => r.id !== mine.id));
    forgetOwnEnrollment();
    void decideEnrollment(server, mine, mine.verificationNumber, false).catch(() => {});
  });
});

/** The requests to ask the person about: not this browser's own. The tray lists them (SPEC §1.1). */
export const deviceRequests = computed(() => requests().filter((r) => !withdrawn.has(r.id) && r.id !== ownEnrollmentRequest()));

/** Requests whose sheet the person closed undecided: they wait in the tray, whose row opens the sheet again. */
const setAside = signal<ReadonlySet<string>>(new Set());

/** A tray row of a new device: its sheet. */
export function showDeviceRequest(id: string): void {
  const next = new Set(untracked(setAside));
  next.delete(id);
  setAside.set(next);
}

// Coming back to the app reads what arrived while away.
effect(() => {
  if (lifecycle() === "active") untracked(refresh);
});

/** A request decided here: it leaves before the computer's word comes back. */
export function forgetDeviceRequest(id: string): void {
  requests.set(untracked(requests).filter((r) => r.id !== id));
}

/** The sheet closed undecided: the request waits in the tray. */
export function putDeviceRequestAside(id: string): void {
  setAside.set(new Set([...untracked(setAside), id]));
}

/** The request the sheet asks about: the oldest not set aside. */
export const shownDeviceRequest = computed(() => deviceRequests().find((r) => !setAside().has(r.id)) ?? null);
