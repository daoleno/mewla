import React, { useEffect } from 'react';
import { useIsFocused, useLocalSearchParams, useRouter } from 'expo-router';
import { useCurrentServer } from '../../store/currentServer';
import { resolveTerminalLink } from '../../services/workerRouteId';

let TerminalScreenImpl: React.ComponentType | null = null;

function getTerminalScreenImpl(): React.ComponentType {
  if (!TerminalScreenImpl) {
    TerminalScreenImpl = require('../../components/terminal/screen/TerminalScreenImpl').default as React.ComponentType;
  }
  return TerminalScreenImpl;
}

function firstParam(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

export default function TerminalScreenRoute() {
  const { hydrated, currentServerId } = useCurrentServer();
  const focused = useIsFocused();
  const router = useRouter();
  const params = useLocalSearchParams<{ id?: string | string[]; serverId?: string | string[] }>();
  const routeId = firstParam(params.id);
  const serverId = firstParam(params.serverId);
  const link = hydrated ? resolveTerminalLink(routeId, serverId, currentServerId) : null;
  const canonical = link !== null && link.id === routeId && link.serverId === serverId;
  useEffect(() => {
    if (!focused || !hydrated || canonical) return;
    // A link opened directly may omit the server: it names the current one.
    // Another server's link, or one naming no Worker route, goes home.
    if (link) router.replace({ pathname: '/terminal/[id]', params: link });
    else router.replace('/(primary)/list');
  }, [focused, hydrated, canonical, link?.id, link?.serverId, router]);
  if (!canonical) return null;
  const Screen = getTerminalScreenImpl();
  return <Screen />;
}
