import { cleanup, effect, Image, lifecycle, reduceMotion, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { PREVIEW_STEP_MS, previewState } from "../../../core/settings/petPreview";
import { Page, Section } from "../kit/kit";
import { PetSprite } from "./PetSprite";
import { choosePet, pet, PETS } from "./pets";

/** Settings → Pet: the chosen pet acting out Brain's states, and every pet to pick from. */
export function PetPage() {
  // A step of the preview every 3.2 s while it can move; a new pet starts from the first.
  const step = signal(0);
  let timer: ReturnType<typeof setInterval> | null = null;
  effect(() => {
    if (timer) clearInterval(timer);
    timer = null;
    pet();
    if (reduceMotion() || lifecycle() !== "active") return;
    untracked(() => step.set(0));
    timer = setInterval(() => step.set(step() + 1), PREVIEW_STEP_MS);
  });
  cleanup(() => timer && clearInterval(timer));

  return (
    <Page title="Pet" testID="pet-page">
      <Section footer="Your pet stands in for Brain on this phone. Friends names your agent after it when you turn Friends on.">
        <View class="items-center gap-1 pb-2 pt-5">
          <PetSprite testID="pet-preview" pet={pet} state={() => previewState(step())} size={112} />
          <Text class="text-lg font-semibold" testID="pet-chosen" role="header">{pet().name}</Text>
        </View>
        <View class="flex-row flex-wrap justify-center px-2 pb-4" role="radiogroup" accessibilityLabel="Pet">
          {PETS.map((item) => (
            <Button
              testID={`pet-${item.id}`}
              role="radio"
              selected={pet().id === item.id}
              accessibilityLabel={item.name}
              onPress={() => choosePet(item.id)}
              class={`w-[20%] min-w-[60px] items-center gap-0.5 rounded-xl border-[1.5px] py-1.5 active:opacity-70 ${pet().id === item.id ? "border-foreground bg-background" : "border-transparent"}`}
            >
              <Image src={item.portrait} style={{ width: 52, height: 52 }} />
              <Text class={`text-xs line-clamp-1 ${pet().id === item.id ? "" : "text-muted-foreground"}`}>{item.name}</Text>
            </Button>
          ))}
        </View>
      </Section>
    </Page>
  );
}
