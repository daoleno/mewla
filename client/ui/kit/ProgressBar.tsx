import { View } from "nsh";
import { PROGRESS_SEGMENTS } from "../../core/sessions/progress";

/**
 * A Worker's progress (SPEC §11 tray row): five 4 pt segments with a 4 pt gap, at most 132 wide, ink for
 * the phases it has reached and `track` for the rest. `filled` comes from `progressSteps(phase)`.
 */
export function ProgressBar(props: { filled: number; testID?: string }) {
  return (
    <View testID={props.testID ?? "progress"} accessibilityLabel={`Step ${props.filled} of ${PROGRESS_SEGMENTS}`}
      class="mt-1.5 w-full max-w-[132px] flex-row gap-1">
      {Array.from({ length: PROGRESS_SEGMENTS }, (_, i) => (
        <View class={`h-1 flex-1 rounded-full ${i < props.filled ? "bg-foreground" : "bg-track"}`} />
      ))}
    </View>
  );
}
