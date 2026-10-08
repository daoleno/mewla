import { useEffect, useRef } from "react";
import { useRouter } from "expo-router";
import { shouldLeaveMissingSession } from "./missingSession";

const MISSING_SESSION_GRACE_MS = 2500;

export function useMissingSessionExit({
  focused,
  resolved,
  sessionListFresh,
  brainHostKnown,
}: {
  focused: boolean;
  resolved: boolean;
  sessionListFresh: boolean;
  brainHostKnown: boolean;
}) {
  const router = useRouter();
  const everResolved = useRef(false);
  if (resolved) everResolved.current = true;
  const leave =
    focused &&
    shouldLeaveMissingSession({
      resolved,
      everResolved: everResolved.current,
      sessionListFresh,
      brainHostKnown,
    });
  useEffect(() => {
    if (!leave) return;
    // A Worker opened right after it was created may not be in the list yet.
    const timer = setTimeout(() => router.replace("/(primary)/list"), MISSING_SESSION_GRACE_MS);
    return () => clearTimeout(timer);
  }, [leave, router]);
}
