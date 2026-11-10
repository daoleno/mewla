import { Image, Text, View } from "nsh";
import { Button } from "nsh/base";
import { Page, Section } from "../kit/kit";
import { choosePet, pet, PETS } from "./pets";

/** Settings → Pet: the chosen pet large, and every pet to pick from. */
export function PetPage() {
  return (
    <Page title="Pet" testID="pet-page">
      <Section footer="Your pet stands in for Brain on this phone. Friends names your agent after it when you turn Friends on.">
        <View class="items-center gap-1 pb-2 pt-5">
          <Image src={pet().portrait} style={{ width: 112, height: 112 }} accessibilityLabel={pet().name} />
          <Text class="text-lg font-semibold" testID="pet-chosen">{pet().name}</Text>
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
