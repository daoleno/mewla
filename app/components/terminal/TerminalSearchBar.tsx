import React, { useEffect, useRef, useState } from 'react';
import { StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import type { TerminalThemeChrome } from '../../constants/terminalThemes';
import { Icon } from '../icons/Icon';
import { terminalSearchCount, type TerminalSearchState } from './terminalSearch';

interface TerminalSearchBarProps {
  chrome: TerminalThemeChrome;
  state: TerminalSearchState;
  onQuery(query: string): void;
  onStep(step: -1 | 1): void;
  onClose(): void;
}

const QUERY_DELAY_MS = 150;

/** Search over the scrollback of the Session on screen. Older matches are
 * the up arrow, newer ones the down arrow. */
export function TerminalSearchBar({ chrome, state, onQuery, onStep, onClose }: TerminalSearchBarProps) {
  const [text, setText] = useState('');
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);

  const change = (next: string) => {
    setText(next);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => onQuery(next), QUERY_DELAY_MS);
  };
  const button = (label: string, icon: 'chevron-up' | 'chevron-down' | 'close', press: () => void, disabled = false) => (
    <TouchableOpacity
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled }}
      disabled={disabled}
      activeOpacity={0.7}
      hitSlop={6}
      onPress={press}
      style={styles.button}
    >
      <Icon name={icon} size={18} color={disabled ? chrome.textMuted : chrome.text} />
    </TouchableOpacity>
  );

  return (
    <View
      style={[styles.bar, { backgroundColor: chrome.overlay, borderColor: chrome.borderStrong }]}
    >
      <Icon name="search" size={16} color={chrome.textMuted} />
      <TextInput
        accessibilityLabel="Search the terminal history"
        autoFocus
        autoCapitalize="none"
        autoCorrect={false}
        returnKeyType="search"
        value={text}
        onChangeText={change}
        onSubmitEditing={() => onStep(-1)}
        placeholderTextColor={chrome.textMuted}
        selectionColor={chrome.text}
        style={[styles.input, { color: chrome.text }]}
      />
      <Text style={[styles.count, { color: chrome.textMuted }]}>{terminalSearchCount(state)}</Text>
      {button('Older match', 'chevron-up', () => onStep(-1), !state.total)}
      {button('Newer match', 'chevron-down', () => onStep(1), !state.total)}
      {button('Close search', 'close', onClose)}
    </View>
  );
}

const styles = StyleSheet.create({
  bar: {
    position: 'absolute',
    top: 8,
    left: 8,
    right: 8,
    minHeight: 44,
    flexDirection: 'row',
    alignItems: 'center',
    borderWidth: 1,
    borderRadius: 22,
    paddingLeft: 14,
    paddingRight: 4,
    gap: 4,
  },
  input: {
    flex: 1,
    fontSize: 15,
    paddingVertical: 6,
    paddingHorizontal: 6,
  },
  count: {
    fontSize: 12,
    minWidth: 34,
    textAlign: 'right',
  },
  button: {
    width: 36,
    height: 36,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
