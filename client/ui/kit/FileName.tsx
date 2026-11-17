import { platform, Text, View } from "nsh";
import { fileNameParts, unbroken } from "../../core/files/fileNameParts";

/**
 * A file name on one line that never widens what holds it: its start gives way with "…" and its end stays, so
 * the extension always shows (`20261117185847-转写_美元债…(2).md`). `class` is the text's look. The browser
 * would end the clamped line at a word (`2026-11-17_meeting-…`, a gap before the end), so there the start
 * is one unbroken run; the phones cut a one-line text at a character already.
 */
export function FileName(props: { name: string; class?: string; testID?: string }) {
  const parts = () => fileNameParts(props.name);
  const head = () => (platform.os === "web" ? unbroken(parts().head) : parts().head);
  return (
    <View testID={props.testID} class="min-w-0 shrink flex-row" accessibilityLabel={props.name}>
      <Text class={`min-w-0 shrink line-clamp-1 ${props.class ?? ""}`}>{head()}</Text>
      <If when={parts().tail}><Text class={`shrink-0 line-clamp-1 ${props.class ?? ""}`}>{parts().tail}</Text></If>
    </View>
  );
}
