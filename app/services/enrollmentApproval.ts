export interface PendingEnrollment {
  id: string;
  deviceName: string;
  platform: string;
  origin: string;
  verificationNumber: string;
  expiresAt: string;
}

export type EnrollmentDecision = "approve" | "deny";

export function verificationChoices(number: string): string[] {
  const normalized = number.trim();
  if (!/^\d{3}$/.test(normalized)) return [];
  const choices = new Set([normalized]);
  const value = Number(normalized);
  for (const offset of [1, -1, 10, -10]) {
    const candidate = String((value + offset + 1000) % 1000).padStart(3, "0");
    choices.add(candidate);
  }
  return [...choices].slice(0, 4);
}

export function canApproveEnrollment(
  request: PendingEnrollment,
  selectedNumber: string,
  now = Date.now(),
): boolean {
  return Date.parse(request.expiresAt) > now && selectedNumber === request.verificationNumber;
}
