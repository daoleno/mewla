// The mock's computer paired, in storage before the app reads it (imported first by FixtureApp.tsx).
import { Storage } from "nsh";
import { storage } from "./scenario";

for (const [key, value] of Object.entries(storage)) void Storage.setItem(key, value);
