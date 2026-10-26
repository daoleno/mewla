import React, { useCallback, useState } from "react";
import { ScrollView, StyleSheet } from "react-native";
import { useFocusEffect } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { useAppColors } from "../../constants/tokens";
import { Button } from "../../components/ui/Button";
import { EmptyState } from "../../components/ui/EmptyState";
import { ListRow, ListSection } from "../../components/ui";
import { FriendsAvatar } from "../../components/friends/FriendsAvatar";
import { friendsCall } from "../../services/friendsAccount";
import { displayName, type FriendsAuthor } from "../../services/friendsApi";
import { useCurrentServer } from "../../store/currentServer";

export default function FriendsBlockedScreen() {
  const colors = useAppColors();
  const { currentServer } = useCurrentServer();
  const [people, setPeople] = useState<FriendsAuthor[] | null>(null);

  const load = useCallback(() => {
    if (!currentServer) return;
    friendsCall<{ people: FriendsAuthor[] }>(currentServer, "GET", "/blocks")
      .then((answer) => setPeople(answer.people))
      .catch(() => setPeople([]));
  }, [currentServer]);
  useFocusEffect(load);

  return (
    <SafeAreaView edges={["bottom"]} style={{ flex: 1, backgroundColor: colors.bgPrimary }}>
      <ScrollView contentContainerStyle={styles.content}>
        {people !== null && people.length === 0 ? (
          <EmptyState size="inline" icon="person-remove" title="No one blocked" detail="People you block, and who block you, are hidden from each other." />
        ) : (
          <ListSection>
            {(people ?? []).map((person) => (
              <ListRow
                key={person.id}
                title={displayName(person)}
                subtitle={`by @${person.handle}`}
                leading={<FriendsAvatar author={person} size={36} />}
                trailing={
                  <Button
                    label="Unblock"
                    size="sm"
                    variant="outlined"
                    onPress={() => {
                      if (!currentServer) return;
                      void friendsCall(currentServer, "DELETE", `/people/${person.id}/block`).then(load);
                    }}
                  />
                }
              />
            ))}
          </ListSection>
        )}
      </ScrollView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({ content: { padding: 16 } });
