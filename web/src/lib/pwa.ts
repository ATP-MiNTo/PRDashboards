/** Registers the service worker that precaches the app shell and shows the offline page. */
export async function registerServiceWorker(
  nav: Pick<Navigator, 'serviceWorker'> | undefined = typeof navigator === 'undefined'
    ? undefined
    : navigator,
): Promise<ServiceWorkerRegistration | null> {
  if (!nav || !('serviceWorker' in nav)) return null;
  try {
    return await nav.serviceWorker.register('/sw.js', { scope: '/' });
  } catch {
    return null;
  }
}
