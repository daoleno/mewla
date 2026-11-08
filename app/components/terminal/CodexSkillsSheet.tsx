import React, {
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  ScrollView,
  StyleSheet,
  type TextInputInstance,
  View,
} from "react-native";
import { useAppColors } from "../../constants/tokens";
import {
  type CodexSkill,
  wsClient,
} from "../../services/websocket";
import { AppText, BottomSheetFrame, EmptyState, IconButton, ListRow, ListSection } from "../ui";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";
import { Icon } from "../icons/Icon";

interface CodexSkillsSheetProps {
  visible: boolean;
  serverId: string;
  cwd?: string;
  onSelectSkill(skill: CodexSkill): void;
  onClose(): void;
}

/** Reads the terminal theme from the surrounding `TerminalThemeScope`. */
export function CodexSkillsSheet({
  visible,
  serverId,
  cwd,
  onSelectSkill,
  onClose,
}: CodexSkillsSheetProps) {
  const colors = useAppColors();
  const [query, setQuery] = useState("");
  const [skills, setSkills] = useState<CodexSkill[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const searchInputRef = useRef<TextInputInstance | null>(null);

  useEffect(() => {
    if (!visible) {
      searchInputRef.current?.blur();
      return;
    }
    const timer = setTimeout(() => {
      searchInputRef.current?.focus();
    }, 90);
    return () => clearTimeout(timer);
  }, [visible]);

  useEffect(() => {
    if (!visible) {
      return;
    }
    let cancelled = false;
    setQuery("");
    setLoading(true);
    setError(null);
    void wsClient
      .getCodexSkills(serverId, { cwd })
      .then((snapshot) => {
        if (cancelled) {
          return;
        }
        setSkills(snapshot.skills);
      })
      .catch((err: any) => {
        if (cancelled) {
          return;
        }
        setSkills([]);
        setError(err?.message || "Could not load skills.");
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [cwd, serverId, visible]);

  const visibleSkills = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (!normalized) {
      return skills;
    }
    return skills.filter((skill) => {
      const haystack = [
        skill.name,
        skill.description || "",
        skill.scope,
        skill.path,
      ].join(" ").toLowerCase();
      return haystack.includes(normalized);
    });
  }, [query, skills]);

  return (
    <BottomSheetFrame
      visible={visible}
      maxHeight="84%"
      cardStyle={styles.sheet}
      contentStyle={styles.content}
      keyboardAvoiding
      onClose={onClose}
    >
      <View style={styles.header}>
        <AppText variant="title" style={styles.headerCopy}>Skills</AppText>
        <IconButton icon="close" tone="input" size={40} accessibilityLabel="Close skills" onPress={onClose} />
      </View>

      <MobileSingleLineInput
        framed
        ref={searchInputRef}
        value={query}
        onChangeText={setQuery}
        placeholder="Search skills"
        accessibilityLabel="Search skills"
        autoCapitalize="none"
        autoCorrect={false}
        autoFocus={visible}
        returnKeyType="search"
        containerStyle={styles.search}
        leading={<Icon name="search" size={16} color={colors.textTertiary} />}
        trailing={query ? (
          <IconButton icon="close-circle-fill" tone="ghost" size={44} iconSize={16} color={colors.textTertiary}
            accessibilityLabel="Clear skill search" onPress={() => setQuery("")} />
        ) : undefined}
      />

      {loading ? (
        <EmptyState size="inline" busy detail="Loading skills" style={styles.state} />
      ) : error ? (
        <EmptyState size="inline" tone="danger" title="Couldn't load skills" detail={error} style={styles.state} />
      ) : visibleSkills.length === 0 ? (
        <EmptyState size="inline" detail="No matching skills" style={styles.state} />
      ) : (
        <ScrollView
          style={styles.list}
          contentContainerStyle={styles.listContent}
          keyboardShouldPersistTaps="handled"
          showsVerticalScrollIndicator={false}
        >
          <ListSection>
            {visibleSkills.map((skill) => (
              <ListRow
                key={`${skill.path}:${skill.name}`}
                title={skill.name}
                subtitle={skill.description || null}
                value={skill.scope}
                icon="add"
                iconColor={skill.enabled ? colors.accent : colors.textTertiary}
                accessibilityLabel={`Use ${skill.name}`}
                onPress={() => onSelectSkill(skill)}
              />
            ))}
          </ListSection>
        </ScrollView>
      )}
    </BottomSheetFrame>
  );
}

const styles = StyleSheet.create({
  sheet: { paddingHorizontal: 14, paddingTop: 10, paddingBottom: 18 },
  content: { flexShrink: 1, minHeight: 0 },
  header: { flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 4, paddingBottom: 10 },
  headerCopy: { flex: 1, minWidth: 0 },
  search: { marginBottom: 8, zIndex: 2 },
  list: { flexShrink: 1, minHeight: 0, maxHeight: 430 },
  listContent: { paddingBottom: 18 },
  state: { minHeight: 148 },
});
