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
  // Independent decoys and shuffled positions keep the matching answer from
  // being identifiable by its position or its distance from its neighbours.
  const choices = [normalized];
  const pool = Array.from({ length: 1000 }, (_, value) => String(value).padStart(3, "0")).filter((value) => value !== normalized);
  for (let index = 0; index < 3; index++) {
    const selected = Math.floor(Math.random() * pool.length);
    choices.push(pool.splice(selected, 1)[0]);
  }
  for (let index = choices.length - 1; index > 0; index--) {
    const selected = Math.floor(Math.random() * (index + 1));
    [choices[index], choices[selected]] = [choices[selected], choices[index]];
  }
  return choices;
}

export function canApproveEnrollment(
  request: PendingEnrollment,
  selectedNumber: string,
  now = Date.now(),
): boolean {
  return Date.parse(request.expiresAt) > now && selectedNumber === request.verificationNumber;
}
