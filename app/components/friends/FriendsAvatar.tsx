import React from "react";
import { View } from "react-native";
import { Image } from "expo-image";
import { useAppTheme } from "../../constants/tokens";
import { AppText } from "../ui/AppText";
import { findPetPack } from "../pets/petModel";
import { DEFAULT_PET_ID, PET_PACKS } from "../pets/petPacks";
import { initials, type FriendsAuthor } from "../../services/friendsApi";

/** A person's pet portrait; a person with no known pet gets their initials. */
export function FriendsAvatar({ author, size = 44 }: { author: Pick<FriendsAuthor, "pet_id" | "agent_name" | "handle">; size?: number }) {
  const { colors } = useAppTheme();
  const known = PET_PACKS.some((pack) => pack.id === author.pet_id);
  const radius = Math.round(size * 0.28);
  if (!known) {
    return (
      <View
        accessible={false}
        style={{ width: size, height: size, borderRadius: size / 2, backgroundColor: colors.surfaceSubtle, alignItems: "center", justifyContent: "center" }}
      >
        <AppText variant="label" tone="secondary" style={{ fontSize: Math.round(size * 0.3) }}>
          {initials(author)}
        </AppText>
      </View>
    );
  }
  const pack = findPetPack(PET_PACKS, author.pet_id, DEFAULT_PET_ID);
  return (
    <View
      accessible={false}
      style={{ width: size, height: size, borderRadius: radius, overflow: "hidden", borderWidth: 1, borderColor: colors.borderSubtle, backgroundColor: colors.bgSurface }}
    >
      <Image source={pack.portrait} contentFit="cover" style={{ width: size, height: size }} />
    </View>
  );
}
