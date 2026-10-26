import React from "react";
import { SafeAreaView } from "react-native-safe-area-context";
import { useAppColors } from "../../constants/tokens";
import { PostList } from "../../components/friends/PostList";

export default function FriendsBookmarksScreen() {
  const colors = useAppColors();
  return (
    <SafeAreaView edges={["bottom"]} style={{ flex: 1, backgroundColor: colors.bgPrimary }}>
      <PostList path="/bookmarks" emptyTitle="No bookmarks" emptyDetail="Tap the bookmark on a post to keep it here." />
    </SafeAreaView>
  );
}
