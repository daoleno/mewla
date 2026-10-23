import React, { useState } from "react";
import { StyleSheet, View } from "react-native";
import { Field } from "./CustomServiceForm";
import { AppText, Button, InlineNotice, ListSection } from "../ui";
import { SegmentedControl } from "../ui/SegmentedControl";
import type { ConnectionRequest, PluginIntegration } from "../../services/connections";

type Network = "mainnet" | "testnet";
const ADDRESS = /^0x[0-9a-fA-F]{40}$/;
const NETWORKS = [{ value: "mainnet", label: "Mainnet" }, { value: "testnet", label: "Testnet" }] as const;

/**
 * A Hyperliquid account is one address on one network. The network is chosen
 * here and shown on the account afterwards; the other network is another
 * account. The API wallet key is optional: without it the account is read only.
 */
export function HyperliquidForm({ plugin, connecting, serverName, onConnect }: {
  plugin: PluginIntegration; connecting: boolean; serverName: string;
  onConnect: (input: NonNullable<ConnectionRequest["input"]>, signIn: boolean) => Promise<void>;
}) {
  const [network, setNetwork] = useState<Network>("mainnet");
  const [address, setAddress] = useState("");
  const [key, setKey] = useState("");
  const [error, setError] = useState("");
  const site = network === "testnet" ? "app.hyperliquid-testnet.xyz/API" : "app.hyperliquid.xyz/API";
  const label = network === "testnet" ? "Testnet" : "Mainnet";
  const connect = async () => {
    const trimmed = address.trim();
    if (!ADDRESS.test(trimmed)) { setError("An address is 0x followed by 40 hexadecimal characters."); return; }
    setError("");
    const credential = key.trim();
    setKey("");
    await onConnect({ integration: plugin.id, network, address: trimmed, credential: credential || undefined }, false);
  };
  return <View>
    <ListSection title="Network" footer={network === "testnet"
      ? "Testnet is Hyperliquid's practice network, with test money only. It is a separate account from mainnet."
      : "Mainnet is the real exchange. Each network is its own account in Mewla."}>
      <View style={styles.segment}>
        <SegmentedControl<Network> accessibilityLabel="Hyperliquid network" value={network} onChange={setNetwork} options={NETWORKS} />
      </View>
    </ListSection>
    <ListSection title="Account" footer="The address whose markets, positions and orders Brain reads. It is public on Hyperliquid.">
      <Field label="Address" accessibilityLabel="Hyperliquid account address" placeholder="0x…" value={address} onChangeText={setAddress} autoCapitalize="none" autoCorrect={false} />
    </ListSection>
    <ListSection title="Trading, optional" footer={`Leave this empty to read only. To let Brain trade on ${label}, create an API wallet at ${site} and paste its private key. An API wallet can trade but cannot withdraw. It is approved per network, so a ${label} key does not work on the other one.`}>
      <Field label="API wallet key" accessibilityLabel="Hyperliquid API wallet key" placeholder="0x… (never your main wallet's key)" secureTextEntry autoCapitalize="none" autoCorrect={false} value={key} onChangeText={setKey} />
    </ListSection>
    {network === "testnet" ? <InlineNotice tone="warning" title="Testnet" detail="Everything Brain does with this account happens on Hyperliquid's test network." style={styles.notice} /> : null}
    {error ? <InlineNotice tone="danger" title="Check the form" detail={error} style={styles.notice} /> : null}
    <Button label={`Connect ${label}`} variant="filled" size="lg" block loading={connecting} disabled={!address.trim()} onPress={() => void connect()} />
    <AppText variant="caption" tone="tertiary" style={styles.note}>The key stays on {serverName}, is never shown again and is removed with the account. Brain only places orders when you ask, and every change is off until you allow it.</AppText>
  </View>;
}

const styles = StyleSheet.create({
  segment: { paddingHorizontal: 16, paddingVertical: 10 },
  notice: { marginBottom: 16 },
  note: { paddingHorizontal: 16, paddingTop: 10 },
});
