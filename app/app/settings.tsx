import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Alert,
  AppState,
  KeyboardAvoidingView,
  Linking,
  Platform,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import * as Haptics from "expo-haptics";
import {
  SafeAreaView,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
import * as Clipboard from "expo-clipboard";
import * as DocumentPicker from "expo-document-picker";
import {
  BarcodeScanningResult,
  BarcodeType,
  CameraView,
  scanFromURLAsync,
  useCameraPermissions,
} from "expo-camera";
import {
  ContinuousCorners,
  Radii,
  TypeScale,
  Typography,
  UiTextMetrics,
  useAppColors,
  useAppTheme,
  shadow,
  type AppColors,
  ContentWidth,
} from "../constants/tokens";
import type { ResolvedTheme } from "../theme";
import { DARK_APP_COLORS } from "../theme/primitives";
import { importConnection } from "../services/importConnection";
import { PairingCancelledError } from "../services/pairingScope";
import {
  closePairPresentation,
  completePairImport,
  createClosedPairPresentation,
  lockPairScanner,
  openPairEditor,
  openPairScanner,
  resolvePairPresentationDismiss,
  returnToPairEditor,
  unlockPairScanner,
  type PairPresentationState,
} from "../services/pairPresentation";
import {
  attemptDismissPairScanner,
  createPairScanClaim,
  isPairScanClaimHeld,
  releasePairScan,
  tryClaimPairScan,
} from "../services/pairScanClaim";
import { wsClient } from "../services/websocket";
import type { TelegramConnectionStatus } from "../services/websocket";
import {
  useWorkerServerSummary,
} from "../store/workers";
import * as Storage from "../services/storage";
import { AnimatedPressable } from "../components/ui/AnimatedPressable";
import {
  AppText,
  Button,
  ListRow,
  ListSection,
} from "../components/ui";
import { AppearanceSection, PetSection } from "../components/settings/AppearanceSection";
import { AboutSection } from "../components/settings/AboutSection";
import { CalendarAlertsSection } from "../components/settings/CalendarAlertsSection";
import { LabsSection } from "../components/settings/LabsSection";
import { StatusPill, type StatusTone } from "../components/ui/StatusPill";
import { RisingSheet } from "../components/ui/RisingSheet";
import {
  HEADER_BACK_BUTTON_SIZE,
  HeaderBackButton,
} from "../components/navigation/HeaderBackButton";
import { TelegramConnectionPanel } from "../components/settings/TelegramConnectionPanel";
import { computerStatusWord } from "../components/settings/computerModel";
import { ComputersSection } from "../components/settings/ComputersSection";
import { syncComputers } from "../services/computerSync";
import type { ComputerCheck } from "../services/computerTeam";
import { useCurrentServer } from "../store/currentServer";
import {
  telegramSetupMode,
} from "../components/settings/connectionPresentation";
import { Icon } from "../components/icons/Icon";

const QR_BARCODE_TYPES: BarcodeType[] = ["qr"];
const SCANNER_COLORS = DARK_APP_COLORS;
const TELEGRAM_BOTFATHER_URL = "https://t.me/BotFather";

export default function SettingsScreen() {
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const {
    currentServerId,
    refreshServers: refreshCurrentServers,
    switchCurrentServer,
  } = useCurrentServer();
  const { serverConnections, serverConnectionIssues } = useWorkerServerSummary();
  const colors = useAppColors();
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);
  const params = useLocalSearchParams<{
    addServer?: string;
    refresh?: string;
    pairingRequired?: string;
    pairMode?: string;
  }>();
  const [servers, setServers] = useState<Storage.StoredServer[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [pairPresentation, setPairPresentation] =
    useState<PairPresentationState>(createClosedPairPresentation);
  const [draftImportValue, setDraftImportValue] = useState("");
  const [checks, setChecks] = useState<Record<string, ComputerCheck>>({});
  const [handledAutoOpenToken, setHandledAutoOpenToken] = useState<
    string | null
  >(null);
  const [handledRefreshToken, setHandledRefreshToken] = useState<string | null>(
    null,
  );
  const [cameraPermission, requestCameraPermission] = useCameraPermissions();
  const [cameraMountError, setCameraMountError] = useState<string | null>(null);
  const [pairProgress, setPairProgress] = useState<string | null>(null);
  const scanClaimRef = useRef(createPairScanClaim());

  const releaseScanClaim = useCallback(() => {
    releasePairScan(scanClaimRef.current);
    setPairPresentation((current) => unlockPairScanner(current));
  }, []);

  const tryAcquireScanClaim = useCallback(() => {
    if (!tryClaimPairScan(scanClaimRef.current)) {
      return false;
    }
    setPairPresentation((current) => lockPairScanner(current));
    return true;
  }, []);

  useFocusEffect(
    React.useCallback(() => {
      let cancelled = false;

      (async () => {
        const savedServers = await Storage.getServers();
        if (cancelled) return;

        setServers(savedServers);
        setLoaded(true);
        // Each computer answers for itself; the one in use is not the only
        // one that can be online.
        await syncComputers(async (nextChecks, renamed) => {
          if (cancelled) return;
          setChecks(nextChecks);
          if (renamed) {
            setServers(await Storage.getServers());
            await refreshCurrentServers();
          }
        });
      })();

      return () => {
        cancelled = true;
      };
    }, []),
  );

  useEffect(() => {
    if (
      !loaded ||
      !params.addServer ||
      handledAutoOpenToken === params.addServer
    )
      return;
    openCreateServer();
    if (params.pairMode === "scanner") openScanner();
    setHandledAutoOpenToken(params.addServer);
  }, [handledAutoOpenToken, loaded, params.addServer, params.pairMode]);

  useEffect(() => {
    if (!loaded || !params.refresh || handledRefreshToken === params.refresh)
      return;
    void refreshServers();
    setHandledRefreshToken(params.refresh);
  }, [handledRefreshToken, loaded, params.refresh]);

  const refreshServers = async (preferredServerId?: string) => {
    setServers(await Storage.getServers());
    await refreshCurrentServers(preferredServerId);
  };

  const openCreateServer = () => {
    setDraftImportValue("");
    setCameraMountError(null);
    setPairPresentation(openPairEditor());
  };

  const closeEditor = () => {
    // Never release an in-flight import claim from editor teardown.
    if (isPairScanClaimHeld(scanClaimRef.current)) {
      return;
    }
    setPairPresentation(closePairPresentation());
    setDraftImportValue("");
    setCameraMountError(null);
  };

  const openScanner = () => {
    if (isPairScanClaimHeld(scanClaimRef.current)) {
      return;
    }
    releasePairScan(scanClaimRef.current);
    setCameraMountError(null);
    setPairPresentation((current) => openPairScanner(current));
  };

  const closeScanner = () => {
    // Ref is authority: Done/backdrop/system-back must not dismiss or release
    // while import claim is held — import finally owns release.
    if (attemptDismissPairScanner(scanClaimRef.current) === "blocked") {
      return;
    }
    setCameraMountError(null);
    setPairPresentation((current) => returnToPairEditor(current));
  };

  const handlePairPresentationDismiss = () => {
    // Check claim before reading React mode — ref wins over lagging state.
    if (attemptDismissPairScanner(scanClaimRef.current) === "blocked") {
      return;
    }
    if (
      resolvePairPresentationDismiss(pairPresentation.mode) ===
      "return-to-editor"
    ) {
      setCameraMountError(null);
      setPairPresentation((current) => returnToPairEditor(current));
      return;
    }
    closeEditor();
  };

  const importServer = async (rawValue: string) => {
    // A failure keeps the scan claim until its alert is dismissed: the live
    // camera still sees the same QR and would resubmit a spent one-time code.
    let releaseAfterAlert = false;
    try {
      const savedServer = await importConnection(rawValue, {
        onImported: async (importedServer) => {
          await refreshServers(importedServer.id);
          void syncComputers(async (nextChecks) => {
            setChecks(nextChecks);
            setServers(await Storage.getServers());
            await refreshCurrentServers();
          });
        },
        onProgress: setPairProgress,
      });

      if (!savedServer) {
        Alert.alert(
          "Not a pairing link",
          "Paste the link printed by mewla pair, or scan its QR code.",
        );
        return false;
      }

      // Paste, image, and camera share one success path: release the single Modal.
      setPairPresentation(completePairImport());
      setDraftImportValue("");
      setCameraMountError(null);
      if (params.pairingRequired === "1") {
        router.dismissAll();
        router.replace({ pathname: "/onboarding", params: { paired: "1" } });
      }
      return true;
    } catch (error: any) {
      if (error instanceof PairingCancelledError) {
        setPairPresentation((current) => returnToPairEditor(current));
        return false;
      }
      releaseAfterAlert = true;
      Alert.alert(
        "Pairing failed",
        error?.message || "Could not pair with that daemon.",
        [{ text: "OK", onPress: releaseScanClaim }],
        { cancelable: true, onDismiss: releaseScanClaim },
      );
      return false;
    } finally {
      setPairProgress(null);
      if (!releaseAfterAlert) {
        releaseScanClaim();
      }
    }
  };

  const handleImportDraft = async () => {
    let rawValue = draftImportValue.trim();
    if (!rawValue) {
      rawValue = (await Clipboard.getStringAsync()).trim();
    }
    if (!rawValue) {
      Alert.alert(
        "Pairing link required",
        "Paste the link printed by mewla pair, or scan its QR code.",
      );
      return;
    }
    await importServer(rawValue);
  };

  const handleScanResult = async ({ data }: BarcodeScanningResult) => {
    // Sync claim gate: same-frame camera callbacks cannot both enter import.
    if (!tryAcquireScanClaim()) return;
    await importServer(data || "");
  };

  const handlePickScannerImage = async () => {
    // Claim before DocumentPicker so live onBarcodeScanned is disabled while
    // the system picker is open (UI lock + atomic ref owner).
    if (!tryAcquireScanClaim()) return;

    let importing = false;
    try {
      const result = await DocumentPicker.getDocumentAsync({
        type: ["image/*"],
        copyToCacheDirectory: true,
      });
      if (result.canceled || !result.assets?.length) {
        return;
      }

      const asset = result.assets[0];
      if (!asset.uri) {
        throw new Error("Selected image is not available.");
      }

      const matches = await scanFromURLAsync(asset.uri, QR_BARCODE_TYPES);
      const qrMatch = matches.find((item) => (item.data || "").trim());
      if (!qrMatch?.data) {
        Alert.alert(
          "QR not found",
          "No QR code was detected in that image. Use a tighter crop with the QR filling most of the frame.",
        );
        return;
      }

      // importServer owns the claim from here, including its failure alert.
      importing = true;
      await importServer(qrMatch.data);
    } catch (error: any) {
      Alert.alert(
        "Image scan failed",
        error?.message || "Could not read a QR code from that image.",
      );
    } finally {
      if (!importing) {
        releaseScanClaim();
      }
    }
  };

  if (!loaded) return null;

  return (
    <SafeAreaView style={styles.container} edges={[]}>
      <ScrollView
        style={styles.scrollView}
        contentContainerStyle={[
          styles.content,
          { paddingBottom: Math.max(insets.bottom, 20) + 12 },
        ]}
        showsVerticalScrollIndicator={false}
      >
        <View style={styles.contentInner}>
          <SettingsSectionHeader>Computers</SettingsSectionHeader>
          <ComputersSection
            computers={servers.map((server) => ({
              id: server.id,
              name: server.name,
              status: computerStatusWord({
                current: server.id === currentServerId,
                connection: serverConnections[server.id],
                issue: serverConnectionIssues[server.id]?.code,
                check: checks[server.id],
              }),
            }))}
            onOpen={(id) => router.push({ pathname: "/settings/computer", params: { id } })}
            onPair={() => {
              void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
              openCreateServer();
            }}
          />

          <SettingsSectionHeader>Channels</SettingsSectionHeader>
          {currentServerId ? (
            <TelegramConnectionRow
              key={currentServerId || "no-current-server"}
              serverId={currentServerId}
              connected={serverConnections[currentServerId] === "connected"}
            />
          ) : (
            <ListSection>
              <ListRow title="Telegram" subtitle="No current server" icon="paper-plane" disabled />
            </ListSection>
          )}

          <SettingsSectionHeader>Agents</SettingsSectionHeader>
          <ListSection>
            <ListRow
              title="Model Providers"
              subtitle={currentServerId ? undefined : "No current server"}
              icon="key"
              accessory="chevron"
              accessibilityLabel="Model Providers"
              accessibilityHint="Choose how Codex and Claude connect to models"
              onPress={() => router.push("/settings/model-providers")}
            />
          </ListSection>

          {Platform.OS === "web" ? null : (
            <>
              <SettingsSectionHeader>Labs</SettingsSectionHeader>
              <LabsSection />
            </>
          )}

          <SettingsSectionHeader>Calendar</SettingsSectionHeader>
          <CalendarAlertsSection />

          <SettingsSectionHeader>Appearance</SettingsSectionHeader>
          <AppearanceSection />

          <SettingsSectionHeader>Pet</SettingsSectionHeader>
          <PetSection />

          <SettingsSectionHeader>About</SettingsSectionHeader>
          <AboutSection />
        </View>
      </ScrollView>

      {/* Unified Pair/Edit presentation: one RisingSheet Modal, editor|scanner modes */}
      <RisingSheet
        visible={pairPresentation.mode !== "closed"}
        onClose={handlePairPresentationDismiss}
        layout={pairPresentation.mode === "scanner" ? "fullscreen" : "card"}
        cardStyle={
          pairPresentation.mode === "scanner"
            ? styles.scannerSheetCard
            : styles.modalCard
        }
        avoidKeyboard={pairPresentation.mode === "editor"}
      >
        {pairPresentation.mode === "scanner" ? (
          <View
            style={[
              styles.scannerScreen,
              {
                paddingTop: Math.max(insets.top, 20),
                paddingBottom: Math.max(insets.bottom, 20),
              },
            ]}
            accessibilityViewIsModal
          >
            <View style={styles.scannerHeader}>
              <Text style={styles.scannerTitle} accessibilityRole="header">
                Scan Pairing QR
              </Text>
              <AnimatedPressable
                preset="press"
                scale={0.9}
                style={[
                  styles.scannerCloseButton,
                  pairPresentation.scannerLocked && styles.scannerBtnDisabled,
                ]}
                accessibilityRole="button"
                accessibilityLabel="Close QR scanner"
                accessibilityState={{
                  disabled: pairPresentation.scannerLocked,
                }}
                disabled={pairPresentation.scannerLocked}
                onPress={() => {
                  Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                  closeScanner();
                }}
              >
                <Icon
                  name="close"
                  size={24}
                  color={SCANNER_COLORS.textPrimary}
                />
              </AnimatedPressable>
            </View>

            {cameraMountError ? (
              <View style={styles.scannerNoticeCard}>
                <Text style={styles.scannerNoticeTitle}>
                  Camera unavailable
                </Text>
                <Text style={styles.scannerNoticeText}>{cameraMountError}</Text>
              </View>
            ) : !cameraPermission ? (
              <View style={styles.scannerNoticeCard}>
                <Text style={styles.scannerNoticeTitle}>Loading camera</Text>
              </View>
            ) : !cameraPermission.granted ? (
              <View style={styles.scannerNoticeCard}>
                <Text style={styles.scannerNoticeTitle}>
                  Camera permission required
                </Text>
                <Text style={styles.scannerNoticeText}>
                  Allow camera access to scan a mewla pairing QR code.
                </Text>
                <AnimatedPressable
                  style={[
                    styles.scannerPrimaryBtn,
                    styles.scannerPermissionBtn,
                  ]}
                  preset="press"
                  scale={0.96}
                  accessibilityRole="button"
                  accessibilityLabel="Grant camera access"
                  onPress={() => void requestCameraPermission()}
                >
                  <Text style={styles.scannerPrimaryBtnText}>
                    Grant Camera Access
                  </Text>
                </AnimatedPressable>
              </View>
            ) : (
              <>
                <View style={styles.scannerViewport}>
                  <CameraView
                    style={styles.scannerCamera}
                    facing="back"
                    barcodeScannerSettings={{ barcodeTypes: ["qr"] }}
                    onBarcodeScanned={
                      pairPresentation.scannerLocked
                        ? undefined
                        : handleScanResult
                    }
                    onMountError={(event) => {
                      const message =
                        event?.message ||
                        "The camera could not start. Pick a QR image instead, or paste the pairing link.";
                      setCameraMountError(message);
                    }}
                  />
                  <View pointerEvents="none" style={styles.scannerOverlay}>
                    <View style={styles.scannerMaskTop} />
                    <View style={styles.scannerMaskMiddle}>
                      <View style={styles.scannerMaskSide} />
                      <View style={styles.scannerFrame}>
                        <View style={styles.scannerFrameCornerTopLeft} />
                        <View style={styles.scannerFrameCornerTopRight} />
                        <View style={styles.scannerFrameCornerBottomLeft} />
                        <View style={styles.scannerFrameCornerBottomRight} />
                      </View>
                      <View style={styles.scannerMaskSide} />
                    </View>
                    <View style={styles.scannerMaskBottom} />
                  </View>
                </View>
              </>
            )}

            {pairProgress ? (
              <Text
                style={styles.scannerNoticeText}
                accessibilityLiveRegion="polite"
              >
                {pairProgress}
              </Text>
            ) : null}

            <View style={styles.scannerActions}>
              <AnimatedPressable
                style={[
                  styles.scannerSecondaryBtn,
                  pairPresentation.scannerLocked && styles.scannerBtnDisabled,
                ]}
                preset="press"
                scale={0.96}
                disabled={pairPresentation.scannerLocked}
                accessibilityRole="button"
                accessibilityLabel={
                  pairPresentation.scannerLocked
                    ? "Reading QR image"
                    : "Pick QR image"
                }
                accessibilityState={{
                  disabled: pairPresentation.scannerLocked,
                  busy: pairPresentation.scannerLocked,
                }}
                onPress={() => void handlePickScannerImage()}
              >
                <Text
                  style={[
                    styles.scannerSecondaryBtnText,
                    pairPresentation.scannerLocked &&
                      styles.scannerDisabledBtnText,
                  ]}
                >
                  {pairProgress
                    ? "Pairing..."
                    : pairPresentation.scannerLocked
                      ? "Reading Image..."
                      : "Pick QR Image"}
                </Text>
              </AnimatedPressable>
              <AnimatedPressable
                style={[
                  styles.scannerPrimaryBtn,
                  pairPresentation.scannerLocked && styles.scannerBtnDisabled,
                ]}
                preset="press"
                scale={0.96}
                accessibilityRole="button"
                accessibilityLabel="Close QR scanner"
                accessibilityState={{
                  disabled: pairPresentation.scannerLocked,
                }}
                disabled={pairPresentation.scannerLocked}
                onPress={() => {
                  Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                  closeScanner();
                }}
              >
                <Text style={styles.scannerPrimaryBtnText}>Done</Text>
              </AnimatedPressable>
            </View>
          </View>
        ) : (
          <ScrollView
            style={styles.modalScroll}
            contentContainerStyle={styles.modalScrollContent}
            keyboardShouldPersistTaps="handled"
            showsVerticalScrollIndicator={false}
          >
            <Text style={styles.modalTitle} accessibilityRole="header">
              Pair a computer
            </Text>

            <Text style={styles.importLead}>
              On the computer, run mewla pair. Scan its QR code or paste the link it prints.
            </Text>

            <Text style={styles.fieldLabel}>Pairing link</Text>
            <TextInput
              style={[styles.input, styles.importInput]}
              value={draftImportValue}
              onChangeText={setDraftImportValue}
              accessibilityLabel="Pairing link"
              placeholder="mewla://settings?p=… or https://…/#pair=…"
              placeholderTextColor={colors.textSecondary}
              selectionColor={colors.selectionBackground}
              cursorColor={colors.accentStrong}
              autoCapitalize="none"
              autoCorrect={false}
              multiline
              textAlignVertical="top"
            />
            <View style={styles.modalActions}>
              <Button label="Cancel" variant="plain" style={styles.modalAction} onPress={closeEditor} />
              <Button
                label="Pair"
                variant="filled"
                style={styles.modalAction}
                onPress={() => {
                  void handleImportDraft();
                }}
              />
            </View>

            <Button
              label="Scan QR code"
              icon="qr-code"
              variant="tinted"
              block
              style={styles.scanQrAction}
              onPress={openScanner}
            />
          </ScrollView>
        )}
      </RisingSheet>
    </SafeAreaView>
  );
}

function SettingsSectionHeader({ children }: { children: string }) {
  return (
    <AppText
      variant="label"
      tone="tertiary"
      accessibilityRole="header"
      style={settingsHeaderStyles.header}
    >
      {children}
    </AppText>
  );
}

const settingsHeaderStyles = StyleSheet.create({
  header: {
    paddingHorizontal: 16,
    paddingTop: 4,
    paddingBottom: 6,
  },
});

function TelegramConnectionRow({
  serverId,
  connected,
}: {
  serverId: string | null;
  connected: boolean;
}) {
  const { theme } = useAppTheme();
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(theme), [theme]);
  const [status, setStatus] = useState<TelegramConnectionStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [showToken, setShowToken] = useState(false);
  const [token, setToken] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const ownerActive = useRef(false);
  const ownerEpoch = useRef(0);
  const setupMode = telegramSetupMode(serverId || undefined, connected);
  const activeServerId = setupMode === "direct" && serverId ? serverId : null;
  const visibleStatus = activeServerId ? status : null;

  useFocusEffect(
    useCallback(() => {
      let cancelled = false;
      ownerEpoch.current++;
      ownerActive.current = connected && Boolean(serverId);
      setBusy(false);
      setLoadError(null);
      if (!serverId || !connected) {
        setStatus(null);
        setLoading(false);
        return () => {
          cancelled = true;
          ownerActive.current = false;
          ownerEpoch.current++;
          setToken("");
          setShowToken(false);
        };
      }
      setLoading(true);
      void wsClient
        .getTelegramConnectionStatus(serverId)
        .then((next) => {
          if (!cancelled) setStatus(next);
        })
        .catch(() => {
          if (!cancelled) {
            setStatus(null);
            setLoadError("Telegram status could not be loaded.");
          }
        })
        .finally(() => {
          if (!cancelled) setLoading(false);
        });
      return () => {
        cancelled = true;
        ownerActive.current = false;
        ownerEpoch.current++;
        setToken("");
        setShowToken(false);
      };
    }, [connected, serverId, reload]),
  );

  useEffect(() => {
    if (!expanded || !connected || !serverId || busy) return;
    let cancelled = false;
    let pending = false;
    const refresh = async () => {
      if (pending || AppState.currentState !== "active") return;
      pending = true;
      const epoch = ownerEpoch.current;
      try {
        const next = await wsClient.getTelegramConnectionStatus(serverId);
        if (!cancelled && epoch === ownerEpoch.current) setStatus(next);
      } catch { /* The explicit refresh path surfaces connection failures. */ }
      finally { pending = false; }
    };
    const timer = setInterval(() => void refresh(), 5000);
    const subscription = AppState.addEventListener("change", state => {
      if (state === "active") void refresh();
      else { ownerEpoch.current++; setToken(""); }
    });
    return () => { cancelled = true; clearInterval(timer); subscription.remove(); };
  }, [expanded, connected, serverId, busy]);

  const runStatusMutation = async (
    operation: () => Promise<TelegramConnectionStatus>,
  ) => {
    if (!ownerActive.current) return;
    const epoch = ownerEpoch.current;
    setBusy(true);
    try {
      const next = await operation();
      if (epoch !== ownerEpoch.current) return;
      setStatus(next);
    } catch (error: any) {
      if (epoch !== ownerEpoch.current) return;
      Alert.alert(
        "Telegram connection",
        error?.message || "The connection could not be updated.",
      );
    } finally {
      if (epoch === ownerEpoch.current) setBusy(false);
    }
  };

  const configure = async () => {
    if (!ownerActive.current) return;
    const epoch = ownerEpoch.current;
    const credential = token.trim();
    if (!serverId || !credential) {
      setToken("");
      Alert.alert("Bot token required", "Enter the token issued by BotFather.");
      return;
    }
    setToken("");
    setBusy(true);
    try {
      const next = await wsClient.configureTelegramConnection(
        serverId,
        credential,
      );
      if (epoch !== ownerEpoch.current) return;
      setStatus(next);
      setShowToken(false);
    } catch (error: any) {
      if (epoch !== ownerEpoch.current) return;
      setToken("");
      Alert.alert(
        "Telegram setup failed",
        error?.message || "The bot token could not be verified.",
      );
    } finally {
      // The credential must not survive submission in component state.
      if (epoch === ownerEpoch.current) {
        setToken("");
        setBusy(false);
      }
    }
  };

  const openBotFather = async () => {
    try {
      await Linking.openURL(TELEGRAM_BOTFATHER_URL);
    } catch (error: any) {
      setToken("");
      Alert.alert(
        "BotFather unavailable",
        error?.message || "The official BotFather chat could not be opened.",
      );
    }
  };

  const pasteToken = async () => {
    const epoch = ownerEpoch.current;
    try {
      const value = (await Clipboard.getStringAsync()).trim();
      if (ownerActive.current && epoch === ownerEpoch.current) setToken(value);
    } catch (error: any) {
      setToken("");
      Alert.alert(
        "Clipboard unavailable",
        error?.message || "The bot token could not be pasted.",
      );
    }
  };

  const copyTelegramValue = async (value: string) => {
    try {
      await Clipboard.setStringAsync(value);
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
      return true;
    } catch (error: any) {
      Alert.alert("Clipboard unavailable", error?.message || "The value could not be copied.");
      return false;
    }
  };

  const beginBinding = async () => {
    if (!serverId || !ownerActive.current) return;
    const epoch = ownerEpoch.current;
    setBusy(true);
    try {
      if (!status?.enabled) {
        await wsClient.enableTelegramConnection(serverId);
        if (epoch !== ownerEpoch.current) return;
      }
      const challenge = await wsClient.beginTelegramBinding(serverId);
      if (epoch !== ownerEpoch.current) return;
      await Linking.openURL(challenge.url);
      if (epoch !== ownerEpoch.current) return;
      const next = await wsClient.getTelegramConnectionStatus(serverId);
      if (epoch !== ownerEpoch.current) return;
      setStatus(next);
    } catch (error: any) {
      if (epoch !== ownerEpoch.current) return;
      setToken("");
      Alert.alert(
        "Telegram owner binding",
        error?.message || "The private Telegram chat could not be opened.",
      );
    } finally {
      if (epoch === ownerEpoch.current) setBusy(false);
    }
  };

  const confirmMutation = (
    title: string,
    message: string,
    actionLabel: string,
    operation: () => Promise<TelegramConnectionStatus>,
  ) => {
    Alert.alert(title, message, [
      { text: "Cancel", style: "cancel" },
      {
        text: actionLabel,
        style: "destructive",
        onPress: () => void runStatusMutation(operation),
      },
    ]);
  };


  const stateLabel = visibleStatus
    ? telegramConnectionStateLabel(visibleStatus.state)
    : loading
      ? "Loading"
      : "Unavailable";
  const stateTone: StatusTone = visibleStatus
    ? telegramConnectionStateTone(visibleStatus.state)
    : "neutral";
  const closeDetails = () => {
    ownerEpoch.current++;
    setBusy(false);
    setExpanded(false);
    setToken("");
    setShowToken(false);
  };
  // The sheet sits outside the section so it is not counted as a row.
  return (
    <>
    <ListSection style={styles.channelSection}>
      <ListRow
        title="Telegram"
        // The pill carries a non-neutral state, so the subtitle never repeats it.
        subtitle={visibleStatus?.bot_username ? `@${visibleStatus.bot_username}` : stateTone !== "neutral" ? undefined : stateLabel}
        leading={
          <View style={styles.serverGlyph}>
            <Icon name="paper-plane" size={20} color={colors.textSecondary} />
          </View>
        }
        trailing={
          visibleStatus?.bot_username || stateTone !== "neutral" ? (
            <StatusPill label={stateLabel} tone={stateTone} />
          ) : null
        }
        accessory="chevron"
        accessibilityLabel={`Telegram${
          visibleStatus?.bot_username ? `, @${visibleStatus.bot_username}` : ""
        }, ${stateLabel}`}
        accessibilityHint="Open Telegram details and actions"
        onPress={() => {
          setToken("");
          setShowToken(false);
          setExpanded(true);
        }}
      />
    </ListSection>
      <RisingSheet visible={expanded} onClose={closeDetails} layout="fullscreen" cardStyle={{ backgroundColor: colors.bgPrimary }}>
        <SafeAreaView style={{ flex: 1 }}>
          <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === "ios" ? "padding" : "height"}>
            <View style={styles.detailHeader}>
              <HeaderBackButton accessibilityLabel="Back to Settings" onPress={closeDetails} />
              <Text accessibilityRole="header" numberOfLines={1} style={[styles.detailTitle, { color: colors.textPrimary }]}>Telegram</Text>
              <View style={styles.detailHeaderEnd} />
            </View>
            <TelegramConnectionPanel
              status={visibleStatus} connected={Boolean(activeServerId)} loading={loading} busy={busy}
              error={loadError} token={token} editingToken={showToken}
              onToken={setToken} onPaste={() => void pasteToken()} onConfigure={() => void configure()}
              onBotFather={() => void openBotFather()} onBind={() => void beginBinding()}
              onOpen={() => { if (visibleStatus?.bot_username) void Linking.openURL(`https://t.me/${visibleStatus.bot_username}`).catch(() => Alert.alert("Telegram unavailable", "The bot chat could not be opened.")); }}
              onReconnect={() => { if (activeServerId) void runStatusMutation(() => wsClient.enableTelegramConnection(activeServerId)); }}
              onDisconnect={() => { if (activeServerId) void runStatusMutation(() => wsClient.disableTelegramConnection(activeServerId)); }}
              onEditToken={() => setShowToken(true)}
              onCancelToken={() => { setToken(""); setShowToken(false); }}
              onRetry={() => setReload(value => value + 1)}
              onCopy={copyTelegramValue}
              onRevoke={() => { if (activeServerId) confirmMutation("Unlink Telegram account",
                "Remove the verified Telegram owner and require a new binding?", "Unlink",
                () => wsClient.revokeTelegramOwner(activeServerId)); }}
              onRemove={() => { if (activeServerId) confirmMutation("Remove Telegram bot",
                "Remove this server's token, binding and delivery state? Telegram cloud messages are not deleted.", "Remove",
                () => wsClient.removeTelegramConnection(activeServerId)); }}
            />
          </KeyboardAvoidingView>
        </SafeAreaView>
      </RisingSheet>
    </>
  );
}

function telegramConnectionStateLabel(
  state: TelegramConnectionStatus["state"],
): string {
  switch (state) {
    case "connected":
      return "Connected";
    case "setup_pending":
      return "Setup pending";
    case "degraded":
      return "Degraded";
    case "disabled":
      return "Disabled";
  }
}

function telegramConnectionStateTone(
  state: TelegramConnectionStatus["state"],
): StatusTone {
  switch (state) {
    case "connected":
      return "success";
    case "setup_pending":
      return "warning";
    case "degraded":
      return "danger";
    case "disabled":
      return "neutral";
  }
}

function createStyles(theme: ResolvedTheme) {
  const colors = theme.colors;

  return StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: colors.bgPrimary,
    },
    scrollView: {
      flex: 1,
    },
    content: {
      width: "100%",
      paddingHorizontal: 16,
      paddingTop: 20,
    },
    contentInner: {
      width: "100%",
      maxWidth: ContentWidth.page,
      alignSelf: "center",
    },
    serverGlyph: {
      width: 30,
      height: 30,
      borderRadius: 9,
      ...ContinuousCorners,
      alignItems: "center",
      justifyContent: "center",
    },


    channelSection: {},
    // Mirrors the Stack header: Back, centred title, balancing end slot.
    detailHeader: {
      minHeight: 56,
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
    },
    detailTitle: {
      flex: 1,
      textAlign: "center",
      fontFamily: Typography.uiFontMedium,
      fontSize: 17,
    },
    detailHeaderEnd: {
      width: HEADER_BACK_BUTTON_SIZE,
    },
    noticeCard: {
      marginTop: 12,
      padding: 12,
      borderRadius: Radii.xs,
      borderLeftWidth: 3,
      backgroundColor: colors.bgSurface,
    },
    noticeHeader: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
    },
    noticeTitle: {
      ...UiTextMetrics,
      ...TypeScale.label,
      flex: 1,
      color: colors.textPrimary,
    },
    noticeDetail: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      marginTop: 7,
      color: colors.textSecondary,
    },
    noticeHint: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      marginTop: 7,
      color: colors.textTertiary,
    },
    emptyText: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textPrimary,
    },

    // Modal / editor
    modalCard: {
      borderRadius: Radii.md,
      padding: 20,
      maxWidth: 480,
      width: "100%",
      maxHeight: "92%",
      alignSelf: "center",
      backgroundColor: colors.modalSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
      ...shadow("float", colors.shadowColor),
    },
    scannerSheetCard: {
      flex: 1,
      width: "100%",
      backgroundColor: SCANNER_COLORS.bgPrimary,
    },
    modalScroll: {
      flexGrow: 0,
    },
    modalScrollContent: {
      paddingBottom: 2,
    },
    modalTitle: {
      ...UiTextMetrics,
      ...TypeScale.heading,
      color: colors.textPrimary,
      marginBottom: 18,
    },
    importLead: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textSecondary,
      marginBottom: 18,
    },
    fieldLabel: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textSecondary,
      marginBottom: 7,
    },
    input: {
      ...UiTextMetrics,
      ...TypeScale.mono,
      minHeight: 44,
      backgroundColor: colors.inputBackground,
      borderRadius: Radii.sm,
      paddingHorizontal: 14,
      paddingVertical: 10,
      color: colors.textPrimary,
      borderWidth: 1,
      borderColor: colors.borderStrong,
    },
    importInput: {
      minHeight: 112,
      paddingTop: 10,
    },
    modalActions: {
      flexDirection: "row",
      flexWrap: "wrap",
      justifyContent: "flex-end",
      gap: 10,
      marginTop: 22,
    },
    modalAction: {
      flex: 1,
    },
    scanQrAction: {
      marginTop: 12,
    },

    scannerScreen: {
      flex: 1,
      backgroundColor: SCANNER_COLORS.bgPrimary,
      paddingHorizontal: 16,
    },
    scannerHeader: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      marginBottom: 14,
    },
    scannerCloseButton: {
      width: 44,
      height: 44,
      alignItems: "center",
      justifyContent: "center",
      borderRadius: Radii.pill,
      backgroundColor: SCANNER_COLORS.surfaceSubtle,
    },
    scannerTitle: {
      ...UiTextMetrics,
      ...TypeScale.title,
      flex: 1,
      color: SCANNER_COLORS.textPrimary,
    },
    scannerViewport: {
      flex: 1,
      minHeight: 240,
      borderRadius: Radii.md,
      overflow: "hidden",
      backgroundColor: SCANNER_COLORS.bgPrimary,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: SCANNER_COLORS.border,
    },
    scannerCamera: {
      flex: 1,
      minHeight: 240,
    },
    scannerOverlay: {
      ...StyleSheet.absoluteFill,
    },
    scannerMaskTop: {
      flex: 1,
      backgroundColor: "rgba(0,0,0,0.56)",
    },
    scannerMaskMiddle: {
      height: 220,
      flexDirection: "row",
    },
    scannerMaskSide: {
      flex: 1,
      backgroundColor: "rgba(0,0,0,0.56)",
    },
    scannerFrame: {
      width: 220,
      borderRadius: Radii.md,
      borderWidth: 1,
      borderColor: SCANNER_COLORS.borderStrong,
    },
    scannerMaskBottom: {
      flex: 1,
      backgroundColor: "rgba(0,0,0,0.56)",
    },
    scannerFrameCornerTopLeft: {
      position: "absolute",
      top: -1,
      left: -1,
      width: 30,
      height: 30,
      borderTopWidth: 4,
      borderLeftWidth: 4,
      borderColor: SCANNER_COLORS.focusRing,
      borderTopLeftRadius: Radii.md,
    },
    scannerFrameCornerTopRight: {
      position: "absolute",
      top: -1,
      right: -1,
      width: 30,
      height: 30,
      borderTopWidth: 4,
      borderRightWidth: 4,
      borderColor: SCANNER_COLORS.focusRing,
      borderTopRightRadius: Radii.md,
    },
    scannerFrameCornerBottomLeft: {
      position: "absolute",
      bottom: -1,
      left: -1,
      width: 30,
      height: 30,
      borderBottomWidth: 4,
      borderLeftWidth: 4,
      borderColor: SCANNER_COLORS.focusRing,
      borderBottomLeftRadius: Radii.md,
    },
    scannerFrameCornerBottomRight: {
      position: "absolute",
      bottom: -1,
      right: -1,
      width: 30,
      height: 30,
      borderBottomWidth: 4,
      borderRightWidth: 4,
      borderColor: SCANNER_COLORS.focusRing,
      borderBottomRightRadius: Radii.md,
    },
    scannerActions: {
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 10,
      marginTop: 12,
    },
    scannerNoticeCard: {
      marginTop: 18,
      borderRadius: Radii.md,
      padding: 20,
      backgroundColor: SCANNER_COLORS.surfaceSubtle,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: SCANNER_COLORS.border,
      alignItems: "center",
    },
    scannerNoticeTitle: {
      ...UiTextMetrics,
      ...TypeScale.heading,
      color: SCANNER_COLORS.textPrimary,
      textAlign: "center",
    },
    scannerNoticeText: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      marginTop: 8,
      color: SCANNER_COLORS.textSecondary,
      textAlign: "center",
    },
    scannerPrimaryBtn: {
      flexGrow: 1,
      flexBasis: 132,
      marginTop: 12,
      minHeight: 44,
      borderRadius: Radii.sm,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: SCANNER_COLORS.accent,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: SCANNER_COLORS.accent,
      paddingHorizontal: 16,
    },
    scannerPrimaryBtnText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: SCANNER_COLORS.textOnAccent,
      textAlign: "center",
    },
    scannerPermissionBtn: {
      width: "100%",
      maxWidth: 280,
      flexGrow: 0,
      flexBasis: "auto",
    },
    scannerSecondaryBtn: {
      flexGrow: 1,
      flexBasis: 132,
      marginTop: 12,
      minHeight: 44,
      borderRadius: Radii.sm,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: SCANNER_COLORS.surfacePressed,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: SCANNER_COLORS.borderStrong,
      paddingHorizontal: 16,
    },
    scannerBtnDisabled: {
      backgroundColor: SCANNER_COLORS.disabledSurface,
      borderColor: SCANNER_COLORS.border,
    },
    scannerSecondaryBtnText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: SCANNER_COLORS.textPrimary,
      textAlign: "center",
    },
    scannerDisabledBtnText: {
      color: SCANNER_COLORS.disabledText,
    },
  });
}
