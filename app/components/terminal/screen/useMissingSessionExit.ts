import { useEffect, useRef } from "react";
import { useRouter } from "expo-router";
import { shouldLeaveMissingSession } from "./missingSession";

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
    if (leave) router.replace("/(primary)/list");
  }, [leave, router]);
}
