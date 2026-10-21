import React from "react";
import { ScrollView, StyleSheet } from "react-native";
import { AppText } from "../ui/AppText";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";

/**
 * Select Text: the message's text in a sheet where the platform's own text
 * selection works, as the ChatGPT and Claude apps do. The bubble itself stays
 * under the native long-press menu.
 */
export function MessageTextSheet({ text, onClose }: { text: string | null; onClose(): void }) {
  return (
    <BottomSheetFrame visible={text !== null} onClose={onClose} maxHeight="70%" dragToDismiss={false}>
      <ScrollView style={styles.scroll} contentContainerStyle={styles.content} showsVerticalScrollIndicator={false}>
        <AppText variant="body" selectable>
          {text ?? ""}
        </AppText>
      </ScrollView>
    </BottomSheetFrame>
  );
}

const styles = StyleSheet.create({
  scroll: { flexGrow: 0 },
  content: { paddingHorizontal: 16, paddingBottom: 16 },
});
