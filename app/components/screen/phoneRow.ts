import type { ActionMenuItem } from "../ui/ActionMenu";

/**
 * The Phone's row in Screen's menu: present only where phone use is on, it
 * says who is using the phone, and opens the phone's own live page.
 */
export function phoneMenuItem(
  phone: { available: boolean; enabled: boolean; status: { agent?: string | null } | null },
  open: () => void,
): ActionMenuItem | null {
  if (!phone.available || !phone.enabled) return null;
  return { key: "phone", label: "Phone", icon: "phone", detail: phone.status?.agent || "Ready", onPress: open };
}
