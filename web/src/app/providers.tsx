'use client';

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useEffect, useState, type ReactNode } from 'react';
import { registerServiceWorker } from '@/lib/pwa';

export function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
  });
}

export function Providers({ children }: { children: ReactNode }) {
  const [client] = useState(makeQueryClient);
  useEffect(() => {
    if (process.env.NODE_ENV === 'production') void registerServiceWorker();
  }, []);
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
