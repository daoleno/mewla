import { Image, Text, View, token } from "nsh";
import type { AgentKind } from "../../core/sessions/workerPresentation";
import type { TerminalFlavor } from "../../core/sessions/terminalFlavor";
import { AGENT_ASSETS } from "./agents/assets";
import { Icon } from "./Icon";

/**
 * A Session's agent logo in the Expo app's avatar slot (AgentKindIcon, variant
 * "avatar": a 24pt mark centred in a 40pt slot; Grok and the plain terminal sit
 * in a rounded, bordered tile). `size` is the mark; the slot is size + 16 unless `slot` sets it
 * (a tray row's 28 pt tile).
 *
 * Where agents/*.svg come from (paths copied verbatim, root fill-rule kept):
 * - claude.svg   @lobehub/icons-rn 2.21.0 `Claude.Color` (ClaudeColor), brand #D97757
 * - codex.svg    @lobehub/icons-rn 2.21.0 `Codex.Color` (CodexColor), white tile + gradient
 * - grok.svg     @lobehub/icons-rn 2.21.0 `Grok` (GrokMono)
 * - opencode.svg @lobehub/icons-rn 2.21.0 `OpenCode` (OpenCodeMono)
 * - dsh.svg      @lobehub/icons-rn 2.21.0 `DeepSeek` (DeepSeekMono); Expo draws DSH as DeepSeek
 * - cursor.svg   app/components/icons/CursorMark.tsx (CURSOR_MONO_PATH)
 * - pi.svg       app/components/icons/PiMark.tsx (PI_MARK_PATHS)
 * The lobehub paths live in node_modules/@lobehub/icons-rn/dist/index.js under
 * `displayName = "<Name>Mono|Color"`. Mono marks are filled #000 and tinted
 * with the foreground token (Expo: black/white by theme, textPrimary for Grok);
 * Claude and Codex keep their brand colors. Terminal reuses the "terminal"
 * glyph. Of the terminal flavors only the letter badges (bun, redis, nginx)
 * are drawn; the font-icon flavors fall back to the terminal glyph.
 */
/** `bare`: the mark alone, without the tile a terminal or Grok stands on (the capsule's mark has no base, SPEC §11). */
export function AgentIcon(props: { kind: AgentKind; flavor?: TerminalFlavor; size?: number; slot?: number; bare?: boolean; class?: string }) {
  const size = props.size ?? 24;
  const slot = props.slot ?? size + 16;
  return (
    <View class={`items-center justify-center overflow-hidden ${props.class ?? ""}`} style={{ width: slot, height: slot }}>
      {mark(props.kind, props.flavor ?? "shell", size, props.bare ?? false)}
    </View>
  );
}

const TINTED = new Set<AgentKind>(["cursor", "grok", "pi", "opencode", "dsh"]);

const LETTER_FLAVORS: Partial<Record<TerminalFlavor, { letter: string; background: string; text: string }>> = {
  bun: { letter: "B", background: "#141414", text: "#F9F1E1" },
  redis: { letter: "R", background: "#DC382D", text: "#FFFFFF" },
  nginx: { letter: "N", background: "#009639", text: "#FFFFFF" },
};

function mark(kind: AgentKind, flavor: TerminalFlavor, size: number, bare: boolean) {
  if (kind === "terminal") {
    const letter = LETTER_FLAVORS[flavor];
    if (bare && !letter) return <Icon name="terminal" color="foreground" size={size} />;
    const frame = bare ? size : size + 10;
    if (letter) {
      const fontSize = Math.round(frame * 0.42);
      return (
        <View class="items-center justify-center" style={{ width: frame, height: frame, borderRadius: Math.round(frame * 0.28), backgroundColor: letter.background }}>
          <Text class="font-medium" style={{ color: letter.text, fontSize, lineHeight: fontSize + 2 }}>{letter.letter}</Text>
        </View>
      );
    }
    return (
      <View testID="agent-tile" class="items-center justify-center rounded-xl border border-border bg-muted" style={{ width: frame, height: frame }}>
        <Icon name="terminal" color="muted-foreground" size={size} />
      </View>
    );
  }
  const src = AGENT_ASSETS[kind];
  const image = <If when={TINTED.has(kind)} else={<Image src={src} style={{ width: size, height: size }} />}><Image src={src} style={{ width: size, height: size, tintColor: token("foreground") }} /></If>;
  if (kind !== "grok" || bare) return image;
  return (
    <View class="items-center justify-center rounded-xl border border-border bg-muted" style={{ width: size + 8, height: size + 8 }}>
      {image}
    </View>
  );
}
