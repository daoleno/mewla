import { announce, cleanup, Image, signal, Text, View, windowSize } from "nsh";
import { Button } from "nsh/base";
import type { PetState } from "../../../core/settings/petPreview";
import { Caption, Page } from "../../kit/kit";
import { PetSprite } from "../../pets/PetSprite";
import { choosePet, pet, PETS } from "../../pets/pets";
import { haptic } from "../../shell/haptics";

/** The poses "{name} at work" shows, with their captions (SPEC §15 Picker grid). */
const POSES: readonly { state: PetState; caption: string }[] = [
  { state: "idle", caption: "Quiet" },
  { state: "waking", caption: "Waking" },
  { state: "working", caption: "Working" },
  { state: "delegating", caption: "Delegating" },
  { state: "attention", caption: "Needs you" },
  { state: "offline", caption: "Offline" },
];

/** How long the toast stays (SPEC §11 Toast). */
const TOAST_MS = 1_500;

/**
 * Settings › Pet (SPEC §15 Picker grid): the pets by their portrait, three across on a phone and
 * six on wide; picking applies at once ("{name} moved in"); then "{name} at work", the six poses as
 * stills, so they can be checked before choosing.
 */
export function PetPage() {
  const toast = signal("");
  let timer: ReturnType<typeof setTimeout> | null = null;
  cleanup(() => timer && clearTimeout(timer));
  const pick = (id: string, name: string) => {
    if (pet().id === id) return;
    haptic.selection();
    choosePet(id);
    toast.set(`${name} moved in`);
    announce(`${name} moved in`);
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => toast.set(""), TOAST_MS);
  };
  const across = () => (windowSize().width >= 1024 ? 6 : 3);
  const tile = () => `${100 / across()}%` as `${number}%`;

  return (
    <Page title="Pet" testID="pet-page"
      overlay={toast() ? (
        <View class="absolute left-0 right-0 items-center" style={{ bottom: 88, pointerEvents: "none" }}>
          <View testID="pet-toast" class="rounded-full bg-foreground px-4 py-2.5"><Text class="text-sub font-semibold text-background">{toast()}</Text></View>
        </View>
      ) : undefined}>
      <View class="-mx-1 mt-4 flex-row flex-wrap" role="radiogroup" accessibilityLabel="Pet">
        {PETS.map((item) => (
          <View class="p-1" style={{ width: tile() }}>
            <Button testID={`pet-${item.id}`} role="radio" selected={pet().id === item.id} accessibilityLabel={item.id === PETS[0]!.id ? `${item.name}, default` : item.name}
              onPress={() => pick(item.id, item.name)}
              class={`items-center gap-2 rounded-[20px] border-2 bg-card px-1 pb-3 pt-4 active:opacity-70 ${pet().id === item.id ? "border-foreground" : "border-transparent"}`}>
              <Image src={item.art.portrait} accessibilityElementsHidden style={{ width: 64, height: 64 }} />
              <Text class="text-sub font-semibold line-clamp-1">{item.name}</Text>
            </Button>
          </View>
        ))}
      </View>
      <View class="mt-6">
        <Caption testID="pet-chosen" role="header" class="mb-2 px-4" text={`${pet().name} at work`} />
        <View class="flex-row flex-wrap rounded-(--radius-group) bg-card px-2 py-3">
          {POSES.map((pose) => (
            <View testID={`pet-pose-${pose.state}`} class="items-center gap-1.5 py-2" style={{ width: tile() }}>
              <PetSprite state={() => pose.state} size={52} mode="still" />
              <Text class="text-caption text-muted-foreground">{pose.caption}</Text>
            </View>
          ))}
        </View>
      </View>
    </Page>
  );
}
