"use client";

import { useCallback, useEffect, useState } from "react";
import { useCredentials } from "./session";
import type { Credentials } from "./types";

interface Resource<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  reload: () => void;
}

/** useResource loads once and exposes an explicit reload, so a mutation can
 *  revalidate deliberately rather than through a refetch-on-focus surprise. */
export function useResource<T>(fetcher: (creds: Credentials) => Promise<T>): Resource<T> {
  const creds = useCredentials();
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);

    fetcher(creds)
      .then((result) => {
        if (!cancelled) {
          setData(result);
          setError(null);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Request failed");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
    // fetcher is defined at module scope by callers, so it is stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [creds, nonce]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  return { data, error, loading, reload };
}
