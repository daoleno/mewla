// What's New's newest scenes alone, in their frames on the sheet's canvas, for frame strips of their loops
// (client/scripts/shots/whatsnew.js). Every scene, whatever platform draws the page. Never bundled.
import { mount, Scroll, Text, View } from "nsh";
import { Scene } from "../scenes";

const NAMES = ["mac-app", "calendar-day", "triggers-page"];

mount(() => (
  <Scroll keyboardShouldPersistTaps="handled" class="flex-1 bg-sheet" contentClass="gap-4 px-5 py-6">
    {NAMES.map((name) => (
      <View class="gap-1.5">
        <Text class="text-caption font-semibold text-muted-foreground">{name}</Text>
        <Scene name={name} version="0.4.23" />
      </View>
    ))}
  </Scroll>
));
