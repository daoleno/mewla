import React from "react";
import { Platform } from "react-native";
import { useRouter } from "expo-router";
import { ListRow, ListSection } from "../ui";
import { useFriendsLabs } from "../../store/friendsLabs";
import { usePhoneUse } from "../../store/phoneUse";
import { phoneStateWord } from "./PhoneSettings";
import { useIPhoneUse } from "../../store/iphoneUse";
import { iphoneOffered, iphoneStateWord } from "../../services/iphoneApi";

/** Things still being tried: one row per feature, each opening its own page. */
export function LabsSection() {
  const router = useRouter();
  const friends = useFriendsLabs();
  const phone = usePhoneUse();
  const iphone = useIPhoneUse(15_000);
  // The computer reads and signs; the web build has no paired phone to ask.
  if (Platform.OS === "web") return null;

  return (
    <ListSection>
      <ListRow
        title="Friends"
        value={friends.enabled ? "On" : "Off"}
        icon="people"
        accessory="chevron"
        onPress={() => router.push("/settings/friends")}
      />
      {phone.available ? (
        <ListRow
          title="Phone use"
          value={phone.enabled ? phoneStateWord(phone.ready, phone.status?.state) : "Off"}
          icon="phone"
          accessory="chevron"
          onPress={() => router.push("/settings/phone")}
        />
      ) : null}
      {iphoneOffered(iphone.status) ? (
        <ListRow
          title="iPhone use"
          value={iphoneStateWord(iphone.status)}
          icon="phone"
          accessory="chevron"
          onPress={() => router.push("/settings/iphone")}
        />
      ) : null}
    </ListSection>
  );
}
