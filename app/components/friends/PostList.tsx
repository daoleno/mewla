import React, { useCallback, useEffect, useRef, useState, type ReactElement } from "react";
import { ActivityIndicator, FlatList, RefreshControl, StyleSheet, View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import { EmptyState } from "../ui/EmptyState";
import { PostCard } from "./PostCard";
import { useFriendsActions } from "./useFriendsActions";
import { friendsCall } from "../../services/friendsAccount";
import type { FriendsPost } from "../../services/friendsApi";
import { useCurrentServer } from "../../store/currentServer";

interface PostListProps {
  /** The service path of the list, for example /feed?scope=following. */
  path: string;
  header?: ReactElement | null;
  emptyTitle: string;
  emptyDetail?: string;
  /** Bumped by a screen to load the list again. */
  reloadToken?: number;
}

interface Page {
  posts: FriendsPost[];
  next: string;
}

function withCursor(path: string, before: string): string {
  if (!before) return path;
  return `${path}${path.includes("?") ? "&" : "?"}before=${encodeURIComponent(before)}`;
}

/** A paged list of posts with like, bookmark, delete, report and block wired in. */
export function PostList({ path, header = null, emptyTitle, emptyDetail, reloadToken = 0 }: PostListProps) {
  const { colors } = useAppTheme();
  const { currentServer } = useCurrentServer();
  const [posts, setPosts] = useState<FriendsPost[] | null>(null);
  const [next, setNext] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const generation = useRef(0);
  // One video plays at a time, and only while its row is on screen.
  const [playingId, setPlayingId] = useState<string | null>(null);
  const playingRef = useRef<string | null>(null);
  playingRef.current = playingId;
  const onViewable = useRef(({ viewableItems }: { viewableItems: { item: FriendsPost }[] }) => {
    const current = playingRef.current;
    if (current && !viewableItems.some((entry) => entry.item.id === current)) setPlayingId(null);
  }).current;
  const playVideo = useCallback((post: FriendsPost) => setPlayingId(post.id), []);
  const stopVideo = useCallback((post: FriendsPost) => setPlayingId((current) => (current === post.id ? null : current)), []);

  const load = useCallback(
    async (append: boolean, cursor: string) => {
      if (!currentServer) return;
      const mine = ++generation.current;
      try {
        const page = await friendsCall<Page>(currentServer, "GET", withCursor(path, cursor));
        if (generation.current !== mine) return;
        setError(null);
        setNext(page.next);
        setPosts((current) => (append && current ? [...current, ...page.posts.filter((p) => !current.some((c) => c.id === p.id))] : page.posts));
      } catch (reason) {
        if (generation.current === mine) setError(reason instanceof Error ? reason.message : "Could not load");
      }
    },
    [currentServer, path],
  );

  useEffect(() => {
    setPosts(null);
    setNext("");
    void load(false, "");
  }, [load, reloadToken]);

  const actions = useFriendsActions({
    onPost: (post) => setPosts((current) => current?.map((p) => (p.id === post.id ? post : p)) ?? current),
    onRemoved: (id) => setPosts((current) => current?.filter((p) => p.id !== id) ?? current),
    onBlocked: (personId) => setPosts((current) => current?.filter((p) => p.author.id !== personId) ?? current),
  });
  const now = Date.now();

  if (posts === null && !error) {
    return (
      <View style={styles.center}>
        <ActivityIndicator color={colors.textTertiary} />
      </View>
    );
  }
  return (
    <FlatList
      data={posts ?? []}
      keyExtractor={(post) => post.id}
      ListHeaderComponent={header ?? undefined}
      renderItem={({ item }) => (
        <PostCard
          post={item}
          now={now}
          onOpen={actions.openPost}
          onOpenPerson={actions.openPerson}
          onLike={(post) => void actions.toggle(post, "like")}
          onBookmark={(post) => void actions.toggle(post, "bookmark")}
          menu={actions.menu}
          videoActive={playingId === item.id}
          onPlayVideo={playVideo}
          onStopVideo={stopVideo}
        />
      )}
      onViewableItemsChanged={onViewable}
      viewabilityConfig={VIEWABILITY}
      windowSize={7}
      removeClippedSubviews
      ListEmptyComponent={
        <EmptyState size="inline" title={error ? "Could not load" : emptyTitle} detail={error ?? emptyDetail} icon={error ? "cloud-offline" : "people"} />
      }
      ListFooterComponent={loadingMore ? <ActivityIndicator color={colors.textTertiary} style={styles.footer} /> : undefined}
      onEndReachedThreshold={0.6}
      onEndReached={() => {
        if (!next || loadingMore) return;
        setLoadingMore(true);
        void load(true, next).finally(() => setLoadingMore(false));
      }}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          tintColor={colors.textTertiary}
          onRefresh={() => {
            setRefreshing(true);
            void load(false, "").finally(() => setRefreshing(false));
          }}
        />
      }
      contentContainerStyle={styles.content}
    />
  );
}

const VIEWABILITY = { itemVisiblePercentThreshold: 40 };

const styles = StyleSheet.create({
  center: { flex: 1, alignItems: "center", justifyContent: "center" },
  content: { flexGrow: 1 },
  footer: { padding: 16 },
});
